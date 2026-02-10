package db

import (
	"context"
	"database/sql"
	"time"
)

const hashRetryAttempts = 8
const hashRetryBackoff = 100 * time.Millisecond

// HashForInode returns the hash for the given (inode, device_id) if any file
// in the same scan already has a non-null hash (e.g. same inode = hardlink).
// Same-scan only; returns empty string and nil error when not found.
// Retries on SQLITE_BUSY when using multiple connections.
func HashForInode(ctx context.Context, database *sql.DB, scanID int64, inode int64, deviceID *int64) (string, error) {
	var out string
	err := RetryOnBusy(ctx, hashRetryAttempts, hashRetryBackoff, func() error {
		var hash sql.NullString
		var innerErr error
		if deviceID == nil {
			innerErr = database.QueryRowContext(ctx,
				"SELECT hash FROM files WHERE scan_id = ? AND inode = ? AND device_id IS NULL AND hash IS NOT NULL LIMIT 1",
				scanID, inode).Scan(&hash)
		} else {
			innerErr = database.QueryRowContext(ctx,
				"SELECT hash FROM files WHERE scan_id = ? AND inode = ? AND device_id = ? AND hash IS NOT NULL LIMIT 1",
				scanID, inode, *deviceID).Scan(&hash)
		}
		if innerErr != nil {
			if innerErr == sql.ErrNoRows {
				return nil
			}
			return innerErr
		}
		if hash.Valid {
			out = hash.String
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return out, nil
}

// HashForInodeFromPreviousScan returns the hash if any file in a different scan
// has the same (inode, device_id), same size, and a non-null hash (unchanged file).
// Same inode+size across scans means we can reuse the hash without reading. Returns empty string when not found.
// Retries on SQLITE_BUSY when using multiple connections.
func HashForInodeFromPreviousScan(ctx context.Context, database *sql.DB, currentScanID int64, inode int64, deviceID *int64, size int64) (string, error) {
	var out string
	err := RetryOnBusy(ctx, hashRetryAttempts, hashRetryBackoff, func() error {
		var hash sql.NullString
		var innerErr error
		if deviceID == nil {
			innerErr = database.QueryRowContext(ctx,
				"SELECT hash FROM files WHERE scan_id != ? AND inode = ? AND device_id IS NULL AND size = ? AND hash IS NOT NULL LIMIT 1",
				currentScanID, inode, size).Scan(&hash)
		} else {
			innerErr = database.QueryRowContext(ctx,
				"SELECT hash FROM files WHERE scan_id != ? AND inode = ? AND device_id = ? AND size = ? AND hash IS NOT NULL LIMIT 1",
				currentScanID, inode, *deviceID, size).Scan(&hash)
		}
		if innerErr != nil {
			if innerErr == sql.ErrNoRows {
				return nil
			}
			return innerErr
		}
		if hash.Valid {
			out = hash.String
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return out, nil
}

// ResetHashStatusHashingToPending sets hash_status to 'pending' for all files in the scan
// that are currently 'hashing'. Call at the start of RunHashPhase to recover from a previous crash.
// Retries on SQLITE_BUSY when using multiple connections.
func ResetHashStatusHashingToPending(ctx context.Context, database *sql.DB, scanID int64) error {
	return RetryOnBusy(ctx, hashRetryAttempts, hashRetryBackoff, func() error {
		_, err := database.ExecContext(ctx,
			"UPDATE files SET hash_status = 'pending' WHERE scan_id = ? AND hash_status = 'hashing'",
			scanID)
		return err
	})
}

// UpdateFileHash sets hash, hash_status = 'done', and hashed_at for the file.
// Retries on SQLITE_BUSY when using multiple connections.
func UpdateFileHash(ctx context.Context, database *sql.DB, fileID int64, hash string, hashedAt time.Time) error {
	hashedAtStr := hashedAt.UTC().Format(time.RFC3339)
	return RetryOnBusy(ctx, hashRetryAttempts, hashRetryBackoff, func() error {
		_, err := database.ExecContext(ctx,
			"UPDATE files SET hash = ?, hash_status = 'done', hashed_at = ? WHERE id = ?",
			hash, hashedAtStr, fileID)
		return err
	})
}

// ResetFileHashStatusToPending sets hash_status back to 'pending' for the given file
// if it is currently 'hashing'. Use when a claimed job fails (e.g. retries exhausted,
// read error) so the file can be picked up again. Retries on SQLITE_BUSY.
func ResetFileHashStatusToPending(ctx context.Context, database *sql.DB, fileID int64) error {
	return RetryOnBusy(ctx, hashRetryAttempts, hashRetryBackoff, func() error {
		_, err := database.ExecContext(ctx,
			"UPDATE files SET hash_status = 'pending' WHERE id = ? AND hash_status = 'hashing'",
			fileID)
		return err
	})
}
