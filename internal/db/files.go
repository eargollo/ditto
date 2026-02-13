package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// File is a single file record (metadata and optional hash). Path may be relative (folder) or full (when joined with folder for display).
type File struct {
	ID         int64
	ScanID     int64   // set when querying by scan (from file_scan)
	FolderID   int64   // folder that contains this file
	Path       string  // relative to folder in DB; full path when selected with folder path for display
	Size       int64
	MTime      int64
	Inode      int64
	DeviceID   *int64
	Hash       *string
	HashStatus string
	HashedAt   *time.Time
}

// UpsertFile inserts or updates a file by (folder_id, path) and returns the file id. Path must be relative to the folder root.
func UpsertFile(ctx context.Context, db *sql.DB, folderID int64, path string, size, mtime, inode int64, deviceID *int64) (int64, error) {
	var deviceVal interface{} = nil
	if deviceID != nil {
		deviceVal = *deviceID
	}
	var id int64
	err := db.QueryRowContext(ctx,
		`INSERT INTO files (folder_id, path, size, mtime, inode, device_id, hash_status)
		 VALUES ($1, $2, $3, $4, $5, $6, 'pending')
		 ON CONFLICT (folder_id, path) DO UPDATE SET size = EXCLUDED.size, mtime = EXCLUDED.mtime, inode = EXCLUDED.inode, device_id = EXCLUDED.device_id
		 RETURNING id`,
		folderID, path, size, mtime, inode, deviceVal).Scan(&id)
	return id, err
}

// InsertFileScan links a file to a scan (ledger). Idempotent: use ON CONFLICT DO NOTHING if needed.
func InsertFileScan(ctx context.Context, db *sql.DB, fileID, scanID int64) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO file_scan (file_id, scan_id) VALUES ($1, $2) ON CONFLICT (file_id, scan_id) DO NOTHING`,
		fileID, scanID)
	return err
}

// FileRow is a single file's metadata for batch insert. Path is relative to folder root.
type FileRow struct {
	Path     string
	Size     int64
	MTime    int64
	Inode    int64
	DeviceID *int64
}

// UpsertFilesBatch inserts or updates multiple files in one round-trip and returns their IDs in the same order.
// Paths must be relative to the folder root. Empty slice returns nil, nil.
func UpsertFilesBatch(ctx context.Context, database *sql.DB, folderID int64, rows []FileRow) ([]int64, error) {
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
		var dev interface{} = nil
		if r.DeviceID != nil {
			dev = *r.DeviceID
		}
		args = append(args, folderID, r.Path, r.Size, r.MTime, r.Inode, dev)
	}
	// #nosec G202 -- placeholders built from len(rows); all values passed as args
	query := `INSERT INTO files (folder_id, path, size, mtime, inode, device_id, hash_status)
		VALUES ` + strings.Join(placeholders, ", ") + `
		ON CONFLICT (folder_id, path) DO UPDATE SET size = EXCLUDED.size, mtime = EXCLUDED.mtime, inode = EXCLUDED.inode, device_id = EXCLUDED.device_id
		RETURNING id`
	rowsResult, err := database.QueryContext(ctx, query, args...)
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
func InsertFileScanBatch(ctx context.Context, database *sql.DB, fileIDs []int64, scanID int64) error {
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
	_, err := database.ExecContext(ctx, query, args...)
	return err
}

// GetFilesByScanID returns all files that appear in the given scan (with full path: folder path || '/' || file path). ScanID is set on each file.
func GetFilesByScanID(ctx context.Context, db *sql.DB, scanID int64) ([]File, error) {
	rows, err := db.QueryContext(ctx,
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
