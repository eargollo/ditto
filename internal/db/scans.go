package db

import (
	"context"
	"database/sql"
	"time"
)

// Scan is a single scan run (metadata and stats; file presence is in file_scan ledger).
type Scan struct {
	ID                 int64
	FolderID           int64     // folder that was scanned
	CreatedAt          time.Time
	CompletedAt        *time.Time
	RootPath           string    // folder path (from join)
	HashStartedAt      *time.Time
	HashCompletedAt    *time.Time
	FileCount          *int64
	ScanSkippedCount   *int64
	HashedFileCount    *int64
	HashedByteCount    *int64
	HashReusedCount    *int64
	HashErrorCount     *int64
}

// CreateScan inserts a new scan for the given folder_id and returns the scan.
func CreateScan(ctx context.Context, database *sql.DB, folderID int64) (*Scan, error) {
	var id int64
	err := database.QueryRowContext(ctx,
		`INSERT INTO scans (folder_id, started_at, completed_at) VALUES ($1, $2, NULL) RETURNING id`,
		folderID, NowUTC()).Scan(&id)
	if err != nil {
		return nil, err
	}
	return GetScan(ctx, database, id)
}

// GetScan returns the scan with the given id (with root_path from folders join), or sql.ErrNoRows if not found.
func GetScan(ctx context.Context, database *sql.DB, id int64) (*Scan, error) {
	var s Scan
	var completedAt, hashStartedAt, hashCompletedAt sql.NullTime
	var fileCount, scanSkipped, hashedFileCount, hashedByteCount, hashReused, hashError sql.NullInt64
	err := database.QueryRowContext(ctx,
		`SELECT s.id, s.folder_id, s.started_at, s.completed_at, f.path, s.hash_started_at, s.hash_completed_at,
		 s.file_count, s.scan_skipped_count, s.hashed_file_count, s.hashed_byte_count, s.hash_reused_count, s.hash_error_count
		 FROM scans s JOIN folders f ON s.folder_id = f.id WHERE s.id = $1`,
		id).Scan(&s.ID, &s.FolderID, &s.CreatedAt, &completedAt, &s.RootPath, &hashStartedAt, &hashCompletedAt,
		&fileCount, &scanSkipped, &hashedFileCount, &hashedByteCount, &hashReused, &hashError)
	if err != nil {
		return nil, err
	}
	if completedAt.Valid {
		s.CompletedAt = &completedAt.Time
	}
	if hashStartedAt.Valid {
		s.HashStartedAt = &hashStartedAt.Time
	}
	if hashCompletedAt.Valid {
		s.HashCompletedAt = &hashCompletedAt.Time
	}
	if fileCount.Valid {
		s.FileCount = &fileCount.Int64
	}
	if scanSkipped.Valid {
		s.ScanSkippedCount = &scanSkipped.Int64
	}
	if hashedFileCount.Valid {
		s.HashedFileCount = &hashedFileCount.Int64
	}
	if hashedByteCount.Valid {
		s.HashedByteCount = &hashedByteCount.Int64
	}
	if hashReused.Valid {
		s.HashReusedCount = &hashReused.Int64
	}
	if hashError.Valid {
		s.HashErrorCount = &hashError.Int64
	}
	return &s, nil
}

// UpdateScanCompletedAt sets completed_at, file_count, and scan_skipped_count for the given scan.
func UpdateScanCompletedAt(ctx context.Context, database *sql.DB, scanID int64, fileCount, scanSkippedCount int64) error {
	_, err := database.ExecContext(ctx,
		"UPDATE scans SET completed_at = $1, file_count = $2, scan_skipped_count = $3 WHERE id = $4",
		NowUTC(), fileCount, scanSkippedCount, scanID)
	return err
}

// UpdateScanHashStartedAt sets hash_started_at and clears hash completed/counts. Call at start of RunHashPhase.
func UpdateScanHashStartedAt(ctx context.Context, database *sql.DB, scanID int64) error {
	_, err := database.ExecContext(ctx,
		`UPDATE scans SET hash_started_at = $1, hash_completed_at = NULL, hashed_file_count = NULL,
		 hashed_byte_count = NULL, hash_reused_count = NULL, hash_error_count = NULL WHERE id = $2`,
		NowUTC(), scanID)
	return err
}

// UpdateScanHashCompletedAt sets hash_completed_at and hash-phase counts for the scan.
func UpdateScanHashCompletedAt(ctx context.Context, database *sql.DB, scanID int64, hashedFileCount, hashedByteCount, hashReusedCount, hashErrorCount int64) error {
	_, err := database.ExecContext(ctx,
		`UPDATE scans SET hash_completed_at = $1, hashed_file_count = $2, hashed_byte_count = $3,
		 hash_reused_count = $4, hash_error_count = $5 WHERE id = $6`,
		NowUTC(), hashedFileCount, hashedByteCount, hashReusedCount, hashErrorCount, scanID)
	return err
}

// GetHashedFileCountAndBytes returns the count and sum of sizes for files with hash_status = 'done' in the scan.
func GetHashedFileCountAndBytes(ctx context.Context, database *sql.DB, scanID int64) (fileCount, byteCount int64, err error) {
	err = database.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(f.size), 0) FROM files f
		 JOIN file_scan fs ON f.id = fs.file_id WHERE fs.scan_id = $1 AND f.hash_status = 'done'`,
		scanID).Scan(&fileCount, &byteCount)
	return fileCount, byteCount, err
}

// ListScans returns all scans ordered by started_at descending (newest first), with root_path from folders.
func ListScans(ctx context.Context, database *sql.DB) ([]Scan, error) {
	return listScans(ctx, database, 0)
}

// ListScansRecent returns the most recent scans (limit rows).
func ListScansRecent(ctx context.Context, database *sql.DB, limit int) ([]Scan, error) {
	return listScans(ctx, database, limit)
}

func listScans(ctx context.Context, database *sql.DB, limit int) ([]Scan, error) {
	q := `SELECT s.id, s.folder_id, s.started_at, s.completed_at, f.path, s.hash_started_at, s.hash_completed_at,
	      s.file_count, s.scan_skipped_count, s.hashed_file_count, s.hashed_byte_count, s.hash_reused_count, s.hash_error_count
	      FROM scans s JOIN folders f ON s.folder_id = f.id ORDER BY s.started_at DESC, s.id DESC`
	args := []interface{}{}
	if limit > 0 {
		q += " LIMIT $1"
		args = append(args, limit)
	}
	rows, err := database.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var scans []Scan
	for rows.Next() {
		var s Scan
		var completedAt, hashStartedAt, hashCompletedAt sql.NullTime
		var fileCount, scanSkipped, hashedFileCount, hashedByteCount, hashReused, hashError sql.NullInt64
		if err := rows.Scan(&s.ID, &s.FolderID, &s.CreatedAt, &completedAt, &s.RootPath, &hashStartedAt, &hashCompletedAt,
			&fileCount, &scanSkipped, &hashedFileCount, &hashedByteCount, &hashReused, &hashError); err != nil {
			return nil, err
		}
		if completedAt.Valid {
			s.CompletedAt = &completedAt.Time
		}
		if hashStartedAt.Valid {
			s.HashStartedAt = &hashStartedAt.Time
		}
		if hashCompletedAt.Valid {
			s.HashCompletedAt = &hashCompletedAt.Time
		}
		if fileCount.Valid {
			s.FileCount = &fileCount.Int64
		}
		if scanSkipped.Valid {
			s.ScanSkippedCount = &scanSkipped.Int64
		}
		if hashedFileCount.Valid {
			s.HashedFileCount = &hashedFileCount.Int64
		}
		if hashedByteCount.Valid {
			s.HashedByteCount = &hashedByteCount.Int64
		}
		if hashReused.Valid {
			s.HashReusedCount = &hashReused.Int64
		}
		if hashError.Valid {
			s.HashErrorCount = &hashError.Int64
		}
		scans = append(scans, s)
	}
	return scans, rows.Err()
}

// GetLatestIncompleteScanForFolder returns the most recent scan for the given folder_id that is not fully complete. Returns 0 if none.
func GetLatestIncompleteScanForFolder(ctx context.Context, database *sql.DB, folderID int64) (int64, error) {
	var id int64
	err := database.QueryRowContext(ctx,
		`SELECT id FROM scans WHERE folder_id = $1 AND (completed_at IS NULL OR hash_completed_at IS NULL)
		 ORDER BY started_at DESC, id DESC LIMIT 1`,
		folderID).Scan(&id)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return id, nil
}
