package db

import (
	"context"
	"database/sql"
)

// ClaimNextHashJob atomically claims the next pending hash job for the given scan.
// Only files in a same-size group (size shared by at least two files) are candidates.
// Priority is by size descending (largest first). To add "size × count" priority
// (prioritize groups with more total bytes), order by (size * group_count) DESC
// using a subquery or join that provides the count per size for this scan_id.
// The claimed row is set to
// hash_status = 'hashing' and returned. Returns (nil, nil) when there is no pending job.
// Uses a single UPDATE with subquery and RETURNING (SQLite 3.35+) so concurrent
// workers never receive the same file.
func ClaimNextHashJob(ctx context.Context, db *sql.DB, scanID int64) (*File, error) {
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
