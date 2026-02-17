package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// HashForInode returns the hash for the given (inode, device_id) if any file in the same scan already has a non-null hash (hardlink reuse). Returns "", nil when inode is nil (unknown, e.g. Windows).
func HashForInode(ctx context.Context, database *sql.DB, scanID int64, inode *int64, deviceID *int64) (string, error) {
	if inode == nil {
		return "", nil
	}
	var out string
	var err error
	if deviceID == nil {
		err = database.QueryRowContext(ctx,
			`SELECT f.hash FROM files f JOIN file_scan fs ON f.id = fs.file_id
			 WHERE fs.scan_id = $1 AND f.inode = $2 AND f.device_id IS NULL AND f.hash IS NOT NULL LIMIT 1`,
			scanID, *inode).Scan(&out)
	} else {
		err = database.QueryRowContext(ctx,
			`SELECT f.hash FROM files f JOIN file_scan fs ON f.id = fs.file_id
			 WHERE fs.scan_id = $1 AND f.inode = $2 AND f.device_id = $3 AND f.hash IS NOT NULL LIMIT 1`,
			scanID, *inode, *deviceID).Scan(&out)
	}
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return out, nil
}

// HashForInodeFromPreviousScan returns the hash if any file has the same (inode, device_id), size, and a non-null hash (unchanged file reuse). Returns "", nil when inode is nil.
func HashForInodeFromPreviousScan(ctx context.Context, database *sql.DB, currentScanID int64, inode *int64, deviceID *int64, size int64) (string, error) {
	if inode == nil {
		return "", nil
	}
	var out string
	var err error
	if deviceID == nil {
		err = database.QueryRowContext(ctx,
			`SELECT hash FROM files WHERE inode = $1 AND device_id IS NULL AND size = $2 AND hash IS NOT NULL LIMIT 1`,
			*inode, size).Scan(&out)
	} else {
		err = database.QueryRowContext(ctx,
			`SELECT hash FROM files WHERE inode = $1 AND device_id = $2 AND size = $3 AND hash IS NOT NULL LIMIT 1`,
			*inode, *deviceID, size).Scan(&out)
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

// HashUpdate is one file's hash result for batch update.
type HashUpdate struct {
	FileID   int64
	Hash     string
	HashedAt time.Time
}

// UpdateFileHashBatch updates hash, hash_status, and hashed_at for multiple files in one round-trip.
// Empty slice is a no-op. Use this to reduce DB round-trips (e.g. on NAS where each UPDATE is 100–300ms).
func UpdateFileHashBatch(ctx context.Context, database *sql.DB, updates []HashUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	n := len(updates)
	// UPDATE files f SET hash = v.hash, hash_status = 'done', hashed_at = v.hashed_at
	// FROM (VALUES ($1::bigint, $2::text, $3::timestamptz), ...) AS v(id, hash, hashed_at)
	// WHERE f.id = v.id
	placeholders := make([]string, n)
	args := make([]interface{}, 0, n*3)
	for i := 0; i < n; i++ {
		base := i * 3
		placeholders[i] = fmt.Sprintf("($%d::bigint, $%d::text, $%d::timestamptz)", base+1, base+2, base+3)
		u := &updates[i]
		args = append(args, u.FileID, u.Hash, u.HashedAt.UTC())
	}
	// #nosec G202 -- placeholders built from len(updates); all values in args
	query := `UPDATE files f SET hash = v.hash, hash_status = 'done', hashed_at = v.hashed_at
		FROM (VALUES ` + strings.Join(placeholders, ", ") + `) AS v(id, hash, hashed_at)
		WHERE f.id = v.id`
	_, err := database.ExecContext(ctx, query, args...)
	return err
}

// ResetFileHashStatusToPending sets hash_status back to 'pending' for the given file if it is currently 'hashing'.
func ResetFileHashStatusToPending(ctx context.Context, database *sql.DB, fileID int64) error {
	_, err := database.ExecContext(ctx,
		"UPDATE files SET hash_status = 'pending' WHERE id = $1 AND hash_status = 'hashing'",
		fileID)
	return err
}
