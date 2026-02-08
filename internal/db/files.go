package db

import (
	"context"
	"database/sql"
	"time"
)

// File is a single file record from a scan (metadata and optional hash).
type File struct {
	ID         int64
	ScanID     int64
	Path       string
	Size       int64
	MTime      int64
	Inode      int64
	DeviceID   *int64
	Hash       *string
	HashStatus string
	HashedAt   *time.Time
}

// InsertFile inserts a file record for the given scan. hash and hashed_at are left null;
// hash_status is set to "pending". deviceID may be nil.
func InsertFile(ctx context.Context, db *sql.DB, scanID int64, path string, size, mtime, inode int64, deviceID *int64) error {
	var deviceVal interface{} = nil
	if deviceID != nil {
		deviceVal = *deviceID
	}
	_, err := db.ExecContext(ctx,
		"INSERT INTO files (scan_id, path, size, mtime, inode, device_id, hash, hash_status, hashed_at) VALUES (?, ?, ?, ?, ?, ?, NULL, 'pending', NULL)",
		scanID, path, size, mtime, inode, deviceVal)
	return err
}

// GetFilesByScanID returns all files for the given scan, ordered by id.
func GetFilesByScanID(ctx context.Context, db *sql.DB, scanID int64) ([]File, error) {
	rows, err := db.QueryContext(ctx,
		"SELECT id, scan_id, path, size, mtime, inode, device_id, hash, hash_status, hashed_at FROM files WHERE scan_id = ? ORDER BY id",
		scanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []File
	for rows.Next() {
		var f File
		var deviceID sql.NullInt64
		var hash sql.NullString
		var hashedAt nullRFC3339Time
		if err := rows.Scan(&f.ID, &f.ScanID, &f.Path, &f.Size, &f.MTime, &f.Inode, &deviceID, &hash, &f.HashStatus, &hashedAt); err != nil {
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
		files = append(files, f)
	}
	return files, rows.Err()
}
