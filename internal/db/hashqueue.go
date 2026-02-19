package db

import (
	"context"
	"database/sql"
)

// sizeCandidateSubqueryForScan returns a SQL fragment that selects sizes for which we should hash in this scan:
// any size that appears at least twice in this scan. We only hash files that could be duplicates within the scan.
// The subquery must use the same scan ID parameter as the outer query (e.g. $1 for both).
const sizeCandidateSubqueryForScan = `
		SELECT size FROM files f2
		JOIN file_scan fs2 ON f2.id = fs2.file_id
		WHERE fs2.scan_id = $1
		GROUP BY size HAVING COUNT(*) > 1`

// sizeCandidateSubqueryGlobal selects sizes that have at least two distinct files (non-deleted) across all scans.
// So we only hash when a size is "duplicate" by distinct file count, not by (file, scan) row count (same file in two scans stays unique).
const sizeCandidateSubqueryGlobal = `
		SELECT size FROM files f2
		JOIN file_scan fs2 ON f2.id = fs2.file_id
		WHERE f2.deleted_at IS NULL
		GROUP BY size HAVING COUNT(DISTINCT f2.id) > 1`

// CountHashCandidates returns the number of files in this scan that are hash candidates (same-size logic).
func CountHashCandidates(ctx context.Context, q Querier, scanID int64) (int64, error) {
	var n int64
	err := q.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM files f
		JOIN file_scan fs ON f.id = fs.file_id
		WHERE fs.scan_id = $1 AND f.hash_status = 'pending' AND f.size IN (`+sizeCandidateSubqueryForScan+`)`, scanID).Scan(&n)
	return n, err
}

// CountHashCandidatesGlobal returns the number of distinct pending files (any scan) whose size appears at least twice globally.
// Same file in multiple scans counts once. Used when running the hash phase so we also hash files from previous scans (e.g. scenario 8).
func CountHashCandidatesGlobal(ctx context.Context, q Querier) (int64, error) {
	var n int64
	err := q.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT f.id) FROM files f
		JOIN file_scan fs ON f.id = fs.file_id
		WHERE f.deleted_at IS NULL AND f.hash_status = 'pending' AND f.size IN (`+sizeCandidateSubqueryGlobal+`)`).Scan(&n)
	return n, err
}

const pendingHashJobsQuery = `
	SELECT f.id, $2::bigint, (fo.path || '/' || f.path), f.size, f.mtime, f.inode, f.device_id, f.hash, f.hash_status, f.hashed_at, f.folder_id, f.path
	FROM files f
	JOIN file_scan fs ON f.id = fs.file_id
	JOIN folders fo ON f.folder_id = fo.id
	WHERE fs.scan_id = $1 AND f.hash_status = 'pending'
	AND f.size IN (` + sizeCandidateSubqueryForScan + `)
	ORDER BY f.size DESC`

const pendingHashJobsQueryGlobal = `
	SELECT f.id, fs.scan_id, (fo.path || '/' || f.path), f.size, f.mtime, f.inode, f.device_id, f.hash, f.hash_status, f.hashed_at, f.folder_id, f.path
	FROM files f
	JOIN file_scan fs ON f.id = fs.file_id
	JOIN folders fo ON f.folder_id = fo.id
	WHERE f.deleted_at IS NULL AND f.hash_status = 'pending'
	AND f.size IN (` + sizeCandidateSubqueryGlobal + `)
	ORDER BY f.size DESC`

// ForEachPendingHashJob runs one query to stream all pending hash jobs for the scan. For each row it calls fn.
func ForEachPendingHashJob(ctx context.Context, q Querier, scanID int64, fn func(*File) error) error {
	rows, err := q.QueryContext(ctx, pendingHashJobsQuery, scanID, scanID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var f File
		var inode sql.NullInt64
		var deviceID sql.NullInt64
		var hash sql.NullString
		var hashedAt nullRFC3339Time
		if err := rows.Scan(&f.ID, &f.ScanID, &f.Path, &f.Size, &f.MTime, &inode, &deviceID, &hash, &f.HashStatus, &hashedAt, &f.FolderID, &f.RelPath); err != nil {
			return err
		}
		if inode.Valid {
			v := inode.Int64
			f.Inode = &v
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

// ForEachPendingHashJobGlobal streams all pending hash jobs (any scan) whose size appears at least twice globally.
// Each file's ScanID is set to its actual scan (from file_scan). Used so the hash phase can hash files from previous scans.
func ForEachPendingHashJobGlobal(ctx context.Context, q Querier, fn func(*File) error) error {
	rows, err := q.QueryContext(ctx, pendingHashJobsQueryGlobal)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var f File
		var inode sql.NullInt64
		var deviceID sql.NullInt64
		var hash sql.NullString
		var hashedAt nullRFC3339Time
		if err := rows.Scan(&f.ID, &f.ScanID, &f.Path, &f.Size, &f.MTime, &inode, &deviceID, &hash, &f.HashStatus, &hashedAt, &f.FolderID, &f.RelPath); err != nil {
			return err
		}
		if inode.Valid {
			v := inode.Int64
			f.Inode = &v
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
func ClaimNextHashJob(ctx context.Context, q Querier, scanID int64) (*File, error) {
	row := q.QueryRowContext(ctx, `
		UPDATE files SET hash_status = 'hashing'
		WHERE id = (
			SELECT f.id FROM files f JOIN file_scan fs ON f.id = fs.file_id
			WHERE fs.scan_id = $1 AND f.hash_status = 'pending'
			AND f.size IN (`+sizeCandidateSubqueryForScan+`)
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
	// Load full file row with path (we need path for hashing). Join folders for full path; include folder_id and f.path for path+size reuse.
	row = q.QueryRowContext(ctx,
		`SELECT f.id, $2::bigint, (fo.path || '/' || f.path), f.size, f.mtime, f.inode, f.device_id, f.hash, f.hash_status, f.hashed_at, f.folder_id, f.path
		 FROM files f JOIN folders fo ON f.folder_id = fo.id WHERE f.id = $1`,
		fileID, scanID)
	var f File
	var inode sql.NullInt64
	var deviceID sql.NullInt64
	var hash sql.NullString
	var hashedAt nullRFC3339Time
	if err := row.Scan(&f.ID, &f.ScanID, &f.Path, &f.Size, &f.MTime, &inode, &deviceID, &hash, &f.HashStatus, &hashedAt, &f.FolderID, &f.RelPath); err != nil {
		return nil, err
	}
	if inode.Valid {
		v := inode.Int64
		f.Inode = &v
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

// ClaimNextHashJobGlobal atomically claims the next pending hash job from any scan (size in global duplicate set). Returns (nil, nil) when none.
// The returned file's ScanID is its actual scan (from file_scan).
func ClaimNextHashJobGlobal(ctx context.Context, q Querier) (*File, error) {
	row := q.QueryRowContext(ctx, `
		UPDATE files SET hash_status = 'hashing'
		WHERE id = (
			SELECT f.id FROM files f JOIN file_scan fs ON f.id = fs.file_id
			WHERE f.deleted_at IS NULL AND f.hash_status = 'pending'
			AND f.size IN (`+sizeCandidateSubqueryGlobal+`)
			ORDER BY f.size DESC
			LIMIT 1
		)
		RETURNING id`)
	var fileID int64
	err := row.Scan(&fileID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	// Load full file row with path and actual scan_id from file_scan.
	row = q.QueryRowContext(ctx,
		`SELECT f.id, fs.scan_id, (fo.path || '/' || f.path), f.size, f.mtime, f.inode, f.device_id, f.hash, f.hash_status, f.hashed_at, f.folder_id, f.path
		 FROM files f JOIN file_scan fs ON f.id = fs.file_id JOIN folders fo ON f.folder_id = fo.id WHERE f.id = $1`,
		fileID)
	var f File
	var inode sql.NullInt64
	var deviceID sql.NullInt64
	var hash sql.NullString
	var hashedAt nullRFC3339Time
	if err := row.Scan(&f.ID, &f.ScanID, &f.Path, &f.Size, &f.MTime, &inode, &deviceID, &hash, &f.HashStatus, &hashedAt, &f.FolderID, &f.RelPath); err != nil {
		return nil, err
	}
	if inode.Valid {
		v := inode.Int64
		f.Inode = &v
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
