package db

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// File is a single file record (metadata and optional hash). Path is full when from hash queue (folder path + RelPath); RelPath is the stored relative path for path+size reuse when inode is nil.
type File struct {
	ID         int64
	ScanID     int64   // set when querying by scan (from file_scan)
	FolderID   int64   // folder that contains this file (optional; set when needed for reuse)
	Path       string  // full path when from hash queue (folder path + "/" + RelPath)
	RelPath    string  // relative path (files.path in DB); used for path+size hash reuse when inode is nil
	Size       int64
	MTime      int64
	Inode      *int64
	DeviceID   *int64
	Hash       *string
	HashStatus string
	HashedAt   *time.Time
}

// UpsertFile inserts or updates a file by (folder_id, path) and returns the file id. Path must be relative to the folder root.
func UpsertFile(ctx context.Context, q Querier, folderID int64, path string, size, mtime int64, inode *int64, deviceID *int64) (int64, error) {
	var deviceVal, inodeVal interface{} = nil, nil
	if deviceID != nil {
		deviceVal = *deviceID
	}
	if inode != nil {
		inodeVal = *inode
	}
	var id int64
	err := q.QueryRowContext(ctx,
		`INSERT INTO files (folder_id, path, size, mtime, inode, device_id, hash_status)
		 VALUES ($1, $2, $3, $4, $5, $6, 'pending')
		 ON CONFLICT (folder_id, path) DO UPDATE SET size = EXCLUDED.size, mtime = EXCLUDED.mtime, inode = EXCLUDED.inode, device_id = EXCLUDED.device_id
		 RETURNING id`,
		folderID, path, size, mtime, inodeVal, deviceVal).Scan(&id)
	return id, err
}

// InsertFileScan links a file to a scan (ledger). Idempotent: use ON CONFLICT DO NOTHING if needed.
func InsertFileScan(ctx context.Context, q Querier, fileID, scanID int64) error {
	_, err := q.ExecContext(ctx,
		`INSERT INTO file_scan (file_id, scan_id) VALUES ($1, $2) ON CONFLICT (file_id, scan_id) DO NOTHING`,
		fileID, scanID)
	return err
}

// FileRow is a single file's metadata for batch insert. Path is relative to folder root. Inode may be nil when unknown.
type FileRow struct {
	Path     string
	Size     int64
	MTime    int64
	Inode    *int64
	DeviceID *int64
}

// UpsertFilesBatch inserts or updates multiple files in one round-trip and returns their IDs in the same order.
// Paths must be relative to the folder root. Empty slice returns nil, nil.
func UpsertFilesBatch(ctx context.Context, q Querier, folderID int64, rows []FileRow) ([]int64, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	// Build VALUES ($1..$6,'pending'), ($7..$12,'pending'), ... ON CONFLICT DO UPDATE RETURNING id
	n := len(rows)
	const colsPerRow = 6
	placeholders := make([]string, n)
	args := make([]interface{}, 0, n*colsPerRow)
	for i := 0; i < n; i++ {
		base := i * colsPerRow
		placeholders[i] = fmt.Sprintf("($%d,$%d,$%d,$%d,$%d,$%d,'pending')",
			base+1, base+2, base+3, base+4, base+5, base+6)
		r := &rows[i]
		var dev, inodeVal interface{} = nil, nil
		if r.DeviceID != nil {
			dev = *r.DeviceID
		}
		if r.Inode != nil {
			inodeVal = *r.Inode
		}
		args = append(args, folderID, r.Path, r.Size, r.MTime, inodeVal, dev)
	}
	// #nosec G202 -- placeholders built from len(rows); all values passed as args
	query := `INSERT INTO files (folder_id, path, size, mtime, inode, device_id, hash_status)
		VALUES ` + strings.Join(placeholders, ", ") + `
		ON CONFLICT (folder_id, path) DO UPDATE SET size = EXCLUDED.size, mtime = EXCLUDED.mtime, inode = EXCLUDED.inode, device_id = EXCLUDED.device_id
		RETURNING id`
	rowsResult, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rowsResult.Close()
	ids := make([]int64, 0, n)
	for rowsResult.Next() {
		var id int64
		if err := rowsResult.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rowsResult.Err(); err != nil {
		return nil, err
	}
	if len(ids) != n {
		return nil, fmt.Errorf("UpsertFilesBatch: got %d ids, want %d", len(ids), n)
	}
	return ids, nil
}

// InsertFileScanBatch links multiple files to a scan in one round-trip. Idempotent (ON CONFLICT DO NOTHING).
func InsertFileScanBatch(ctx context.Context, q Querier, fileIDs []int64, scanID int64) error {
	if len(fileIDs) == 0 {
		return nil
	}
	// VALUES ($1,$N), ($2,$N), ($3,$N), ... where N = len(fileIDs)+1
	n := len(fileIDs)
	args := make([]interface{}, 0, n+1)
	for _, id := range fileIDs {
		args = append(args, id)
	}
	args = append(args, scanID)
	scanParam := n + 1
	placeholders := make([]string, n)
	for i := 0; i < n; i++ {
		placeholders[i] = fmt.Sprintf("($%d,$%d)", i+1, scanParam)
	}
	// #nosec G202 -- placeholders built from len(fileIDs); all values passed as args
	query := `INSERT INTO file_scan (file_id, scan_id) VALUES ` + strings.Join(placeholders, ", ") + `
		ON CONFLICT (file_id, scan_id) DO NOTHING`
	_, err := q.ExecContext(ctx, query, args...)
	return err
}

// UpdateFilesDeletedAtNotInScan marks files in the folder that are not in this scan as deleted (sets deleted_at to now if not already set).
// Call after a scan completes. Use with UpdateFilesDeletedAtInScan for the full update.
// Uses NOT EXISTS so the planner can use an anti-join with index on file_scan(scan_id, file_id) instead of materializing a large NOT IN list.
func UpdateFilesDeletedAtNotInScan(ctx context.Context, q Querier, scanID, folderID int64) error {
	_, err := q.ExecContext(ctx,
		`UPDATE files SET deleted_at = COALESCE(deleted_at, (NOW() AT TIME ZONE 'UTC'))
		 WHERE folder_id = $2 AND NOT EXISTS (SELECT 1 FROM file_scan WHERE file_scan.scan_id = $1 AND file_scan.file_id = files.id)`,
		scanID, folderID)
	return err
}

// UpdateFilesDeletedAtInScan sets deleted_at = NULL for files in this scan and resets hash fields to pending when mtime changed.
// Call after UpdateFilesDeletedAtNotInScan. Uses a JOIN so the "in scan" set is computed once.
func UpdateFilesDeletedAtInScan(ctx context.Context, q Querier, scanID int64) error {
	_, err := q.ExecContext(ctx,
		`UPDATE files SET
			deleted_at = NULL,
			hash_status = CASE WHEN (mtime IS DISTINCT FROM hashed_mtime OR hashed_mtime IS NULL) THEN 'pending' ELSE hash_status END,
			hash = CASE WHEN (mtime IS DISTINCT FROM hashed_mtime OR hashed_mtime IS NULL) THEN NULL ELSE hash END,
			hashed_at = CASE WHEN (mtime IS DISTINCT FROM hashed_mtime OR hashed_mtime IS NULL) THEN NULL ELSE hashed_at END,
			hashed_mtime = CASE WHEN (mtime IS DISTINCT FROM hashed_mtime OR hashed_mtime IS NULL) THEN NULL ELSE hashed_mtime END
		 FROM file_scan WHERE files.id = file_scan.file_id AND file_scan.scan_id = $1`,
		scanID)
	return err
}

// UpdateFilesDeletedAtForScan updates deleted_at for all files in the given folder: NULL for files in the scan, now() for files not in the scan.
// Sets hash_status = 'pending' (and clears hash, hashed_at, hashed_mtime) for files in the scan whose mtime changed since last hash (mtime != hashed_mtime) or never hashed.
// Clearing hash avoids stale hashes for files that are no longer duplicate-size candidates; tests must allow mtime to change (e.g. sleep after write) on 1s-resolution filesystems.
// Call after a scan completes (all file_scan rows for that scan are inserted). folderID must be the scan's folder_id.
// Implemented as two updates (not-in-scan, then in-scan) for better performance on large folders.
func UpdateFilesDeletedAtForScan(ctx context.Context, q Querier, scanID, folderID int64) error {
	if err := UpdateFilesDeletedAtNotInScan(ctx, q, scanID, folderID); err != nil {
		return err
	}
	return UpdateFilesDeletedAtInScan(ctx, q, scanID)
}

// GetFilesByScanID returns all files that appear in the given scan (with full path: folder path || '/' || file path). ScanID is set on each file.
func GetFilesByScanID(ctx context.Context, q Querier, scanID int64) ([]File, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT f.id, $2::bigint, (fo.path || '/' || f.path), f.size, f.mtime, f.inode, f.device_id, f.hash, f.hash_status, f.hashed_at
		 FROM files f JOIN file_scan fs ON f.id = fs.file_id JOIN folders fo ON f.folder_id = fo.id
		 WHERE fs.scan_id = $1 ORDER BY f.id`,
		scanID, scanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFiles(rows)
}
