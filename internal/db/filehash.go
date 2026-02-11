package db

import (
	"context"
	"database/sql"
	"time"
)

// HashForInode returns the hash for the given (inode, device_id) if any file in the same scan already has a non-null hash (hardlink reuse).
func HashForInode(ctx context.Context, database *sql.DB, scanID int64, inode int64, deviceID *int64) (string, error) {
	var out string
	var err error
	if deviceID == nil {
		err = database.QueryRowContext(ctx,
			`SELECT f.hash FROM files f JOIN file_scan fs ON f.id = fs.file_id
			 WHERE fs.scan_id = $1 AND f.inode = $2 AND f.device_id IS NULL AND f.hash IS NOT NULL LIMIT 1`,
			scanID, inode).Scan(&out)
	} else {
		err = database.QueryRowContext(ctx,
			`SELECT f.hash FROM files f JOIN file_scan fs ON f.id = fs.file_id
			 WHERE fs.scan_id = $1 AND f.inode = $2 AND f.device_id = $3 AND f.hash IS NOT NULL LIMIT 1`,
			scanID, inode, *deviceID).Scan(&out)
	}
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return out, nil
}

// HashForInodeFromPreviousScan returns the hash if any file has the same (inode, device_id), size, and a non-null hash (unchanged file reuse).
func HashForInodeFromPreviousScan(ctx context.Context, database *sql.DB, currentScanID int64, inode int64, deviceID *int64, size int64) (string, error) {
	var out string
	var err error
	if deviceID == nil {
		err = database.QueryRowContext(ctx,
			`SELECT hash FROM files WHERE inode = $1 AND device_id IS NULL AND size = $2 AND hash IS NOT NULL LIMIT 1`,
			inode, size).Scan(&out)
	} else {
		err = database.QueryRowContext(ctx,
			`SELECT hash FROM files WHERE inode = $1 AND device_id = $2 AND size = $3 AND hash IS NOT NULL LIMIT 1`,
			inode, *deviceID, size).Scan(&out)
	}
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return out, nil
}

// ResetHashStatusHashingToPending sets hash_status to 'pending' for all files in the scan that are currently 'hashing'.
func ResetHashStatusHashingToPending(ctx context.Context, database *sql.DB, scanID int64) error {
	_, err := database.ExecContext(ctx,
		`UPDATE files SET hash_status = 'pending' WHERE id IN (
			SELECT file_id FROM file_scan WHERE scan_id = $1
		 ) AND hash_status = 'hashing'`,
		scanID)
	return err
}

// UpdateFileHash sets hash, hash_status = 'done', and hashed_at for the file.
func UpdateFileHash(ctx context.Context, database *sql.DB, fileID int64, hash string, hashedAt time.Time) error {
	_, err := database.ExecContext(ctx,
		"UPDATE files SET hash = $1, hash_status = 'done', hashed_at = $2 WHERE id = $3",
		hash, hashedAt.UTC(), fileID)
	return err
}

// ResetFileHashStatusToPending sets hash_status back to 'pending' for the given file if it is currently 'hashing'.
func ResetFileHashStatusToPending(ctx context.Context, database *sql.DB, fileID int64) error {
	_, err := database.ExecContext(ctx,
		"UPDATE files SET hash_status = 'pending' WHERE id = $1 AND hash_status = 'hashing'",
		fileID)
	return err
}
