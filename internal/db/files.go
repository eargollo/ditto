package db

import (
	"context"
	"database/sql"
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
