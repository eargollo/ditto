package db

import (
	"context"
	"database/sql"
	"time"
)

const claimRetryAttempts = 8
const claimRetryBackoff = 100 * time.Millisecond

// CountHashCandidates returns the number of files in this scan that are hash candidates
// (in a same-size group). One cheap read at hash phase start for progress logging.
func CountHashCandidates(ctx context.Context, db *sql.DB, scanID int64) (int64, error) {
	var n int64
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM files
		WHERE scan_id = ? AND hash_status = 'pending' AND size IN (
			SELECT size FROM files WHERE scan_id = ? GROUP BY size HAVING COUNT(*) > 1
		)`, scanID, scanID).Scan(&n)
	return n, err
}

const pendingHashJobsQuery = `
	SELECT id, scan_id, path, size, mtime, inode, device_id, hash, hash_status, hashed_at
	FROM files
	WHERE scan_id = ? AND hash_status = 'pending'
	AND size IN (
		SELECT size FROM files WHERE scan_id = ? GROUP BY size HAVING COUNT(*) > 1
	)
	ORDER BY size DESC`

// ForEachPendingHashJob runs one query to stream all pending hash jobs for the scan
// (same set as CountHashCandidates), ordered by size DESC. For each row it calls fn.
// Use from a single producer goroutine; no per-file claim, so no lock contention on claim.
func ForEachPendingHashJob(ctx context.Context, database *sql.DB, scanID int64, fn func(*File) error) error {
	rows, err := database.QueryContext(ctx, pendingHashJobsQuery, scanID, scanID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var f File
		var deviceID sql.NullInt64
		var hash sql.NullString
		var hashedAt nullRFC3339Time
		if err := rows.Scan(&f.ID, &f.ScanID, &f.Path, &f.Size, &f.MTime, &f.Inode, &deviceID, &hash, &f.HashStatus, &hashedAt); err != nil {
			return err
		}
		if deviceID.Valid {
			v := deviceID.Int64
			f.DeviceID = &v
		}
		if hash.Valid {
			s := hash.String
			f.Hash = &s
		}
		f.HashedAt = hashedAt.Ptr()
		if err := fn(&f); err != nil {
			return err
		}
	}
	return rows.Err()
}

// ClaimNextHashJob atomically claims the next pending hash job for the given scan.
// Only files in a same-size group (size shared by at least two files) are candidates.
// Priority is by size descending (largest first). To add "size × count" priority
// (prioritize groups with more total bytes), order by (size * group_count) DESC
// using a subquery or join that provides the count per size for this scan_id.
// The claimed row is set to
// hash_status = 'hashing' and returned. Returns (nil, nil) when there is no pending job.
// Uses a single UPDATE with subquery and RETURNING (SQLite 3.35+) so concurrent
// workers never receive the same file. Retries on SQLITE_BUSY to allow multiple connections.
func ClaimNextHashJob(ctx context.Context, db *sql.DB, scanID int64) (*File, error) {
	var result *File
	err := RetryOnBusy(ctx, claimRetryAttempts, claimRetryBackoff, func() error {
		f, innerErr := claimNextHashJobOnce(ctx, db, scanID)
		if innerErr != nil {
			return innerErr
		}
		result = f
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func claimNextHashJobOnce(ctx context.Context, db *sql.DB, scanID int64) (*File, error) {
	row := db.QueryRowContext(ctx, `
		UPDATE files SET hash_status = 'hashing'
		WHERE id = (
			SELECT id FROM files
			WHERE scan_id = ? AND hash_status = 'pending'
			AND size IN (
				SELECT size FROM files
				WHERE scan_id = ? GROUP BY size HAVING COUNT(*) > 1
			)
			ORDER BY size DESC
			LIMIT 1
		)
		RETURNING id, scan_id, path, size, mtime, inode, device_id, hash, hash_status, hashed_at`,
		scanID, scanID)

	var f File
	var deviceID sql.NullInt64
	var hash sql.NullString
	var hashedAt nullRFC3339Time
	err := row.Scan(&f.ID, &f.ScanID, &f.Path, &f.Size, &f.MTime, &f.Inode, &deviceID, &hash, &f.HashStatus, &hashedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if deviceID.Valid {
		v := deviceID.Int64
		f.DeviceID = &v
	}
	if hash.Valid {
		s := hash.String
		f.Hash = &s
	}
	f.HashedAt = hashedAt.Ptr()
	return &f, nil
}
