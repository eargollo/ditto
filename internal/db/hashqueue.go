package db

import (
	"context"
	"database/sql"
)

// sizeCandidateSubquery returns a SQL fragment (single line) that selects sizes for which we should hash:
// - same size appears more than once in the current scan, OR
// - same size as any already-hashed file (any scan), OR
// - same size as any file in another scan (cross-folder duplicates when size is unique per scan).
// Parameter $1 = current scan_id.
const sizeCandidateSubquery = `
		SELECT f2.size FROM files f2 JOIN file_scan fs2 ON f2.id = fs2.file_id WHERE fs2.scan_id = $1 GROUP BY f2.size HAVING COUNT(*) > 1
		UNION
		SELECT size FROM files WHERE hash_status = 'done'
		UNION
		SELECT f2.size FROM files f2 JOIN file_scan fs2 ON f2.id = fs2.file_id WHERE fs2.scan_id != $1`

// CountHashCandidates returns the number of files in this scan that are hash candidates.
func CountHashCandidates(ctx context.Context, db *sql.DB, scanID int64) (int64, error) {
	var n int64
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM files f
		JOIN file_scan fs ON f.id = fs.file_id
		WHERE fs.scan_id = $1 AND f.hash_status = 'pending' AND f.size IN (`+sizeCandidateSubquery+`)`, scanID).Scan(&n)
	return n, err
}

const pendingHashJobsQuery = `
	SELECT f.id, $2::bigint, (fo.path || '/' || f.path), f.size, f.mtime, f.inode, f.device_id, f.hash, f.hash_status, f.hashed_at
	FROM files f
	JOIN file_scan fs ON f.id = fs.file_id
	JOIN folders fo ON f.folder_id = fo.id
	WHERE fs.scan_id = $1 AND f.hash_status = 'pending'
	AND f.size IN (` + sizeCandidateSubquery + `)
	ORDER BY f.size DESC`

// ForEachPendingHashJob runs one query to stream all pending hash jobs for the scan. For each row it calls fn.
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

// ClaimNextHashJob atomically claims the next pending hash job for the given scan (sets hash_status = 'hashing') and returns it. Returns (nil, nil) when none.
func ClaimNextHashJob(ctx context.Context, db *sql.DB, scanID int64) (*File, error) {
	row := db.QueryRowContext(ctx, `
		UPDATE files SET hash_status = 'hashing'
		WHERE id = (
			SELECT f.id FROM files f JOIN file_scan fs ON f.id = fs.file_id
			WHERE fs.scan_id = $1 AND f.hash_status = 'pending'
			AND f.size IN (`+sizeCandidateSubquery+`)
			ORDER BY f.size DESC
			LIMIT 1
		)
		RETURNING id`,
		scanID)
	var fileID int64
	err := row.Scan(&fileID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	// Load full file row with path (we need path for hashing). Join folders for full path.
	row = db.QueryRowContext(ctx,
		`SELECT f.id, $2::bigint, (fo.path || '/' || f.path), f.size, f.mtime, f.inode, f.device_id, f.hash, f.hash_status, f.hashed_at
		 FROM files f JOIN folders fo ON f.folder_id = fo.id WHERE f.id = $1`,
		fileID, scanID)
	var f File
	var deviceID sql.NullInt64
	var hash sql.NullString
	var hashedAt nullRFC3339Time
	if err := row.Scan(&f.ID, &f.ScanID, &f.Path, &f.Size, &f.MTime, &f.Inode, &deviceID, &hash, &f.HashStatus, &hashedAt); err != nil {
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
