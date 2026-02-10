package db

import (
	"context"
	"database/sql"
	"time"
)

// Scan is a single scan run (metadata and stats; file records are in files table).
type Scan struct {
	ID                 int64
	CreatedAt          time.Time
	CompletedAt        *time.Time
	RootPath           string
	HashStartedAt      *time.Time
	HashCompletedAt    *time.Time
	FileCount          *int64 // files discovered in scan phase
	ScanSkippedCount   *int64 // paths skipped at scan (permission, exclude)
	HashedFileCount    *int64 // files with hash_status = 'done' (computed + reused)
	HashedByteCount    *int64
	HashReusedCount    *int64 // files that got hash from inode/previous scan (no read)
	HashErrorCount     *int64 // files that failed or were skipped during hash phase
}

// CreateScan inserts a new scan with created_at set to now and returns the scan.
// completed_at is left null until the scan run finishes.
func CreateScan(ctx context.Context, db *sql.DB, rootPath string) (*Scan, error) {
	createdAt := time.Now().UTC().Format(time.RFC3339)
	res, err := db.ExecContext(ctx,
		"INSERT INTO scans (created_at, completed_at, root_path) VALUES (?, ?, ?)",
		createdAt, nil, rootPath)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	parsed, _ := time.Parse(time.RFC3339, createdAt)
	return &Scan{ID: id, CreatedAt: parsed, CompletedAt: nil, RootPath: rootPath}, nil
}

// GetScan returns the scan with the given id, or sql.ErrNoRows if not found.
func GetScan(ctx context.Context, database *sql.DB, id int64) (*Scan, error) {
	var rowID int64
	var createdAt rfc3339Time
	var completedAt, hashStartedAt, hashCompletedAt nullRFC3339Time
	var rootPath string
	var fileCount, scanSkipped, hashedFileCount, hashedByteCount, hashReused, hashError sql.NullInt64
	err := database.QueryRowContext(ctx,
		"SELECT id, created_at, completed_at, root_path, hash_started_at, hash_completed_at, file_count, scan_skipped_count, hashed_file_count, hashed_byte_count, hash_reused_count, hash_error_count FROM scans WHERE id = ?", id).
		Scan(&rowID, &createdAt, &completedAt, &rootPath, &hashStartedAt, &hashCompletedAt, &fileCount, &scanSkipped, &hashedFileCount, &hashedByteCount, &hashReused, &hashError)
	if err != nil {
		return nil, err
	}
	s := &Scan{ID: rowID, CreatedAt: createdAt.Time, CompletedAt: completedAt.Ptr(), RootPath: rootPath}
	s.HashStartedAt = hashStartedAt.Ptr()
	s.HashCompletedAt = hashCompletedAt.Ptr()
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
	return s, nil
}

// UpdateScanCompletedAt sets completed_at, file_count, and scan_skipped_count for the given scan. Call when a scan run has finished.
func UpdateScanCompletedAt(ctx context.Context, database *sql.DB, scanID int64, fileCount, scanSkippedCount int64) error {
	completedAt := time.Now().UTC().Format(time.RFC3339)
	_, err := database.ExecContext(ctx,
		"UPDATE scans SET completed_at = ?, file_count = ?, scan_skipped_count = ? WHERE id = ?",
		completedAt, fileCount, scanSkippedCount, scanID)
	return err
}

// UpdateScanHashStartedAt sets hash_started_at to now and clears hash completed/counts (for re-run). Call at start of RunHashPhase.
func UpdateScanHashStartedAt(ctx context.Context, database *sql.DB, scanID int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := database.ExecContext(ctx,
		"UPDATE scans SET hash_started_at = ?, hash_completed_at = NULL, hashed_file_count = NULL, hashed_byte_count = NULL, hash_reused_count = NULL, hash_error_count = NULL WHERE id = ?",
		now, scanID)
	return err
}

// UpdateScanHashCompletedAt sets hash_completed_at and hash-phase counts for the scan. Call when RunHashPhase finishes.
func UpdateScanHashCompletedAt(ctx context.Context, database *sql.DB, scanID int64, hashedFileCount, hashedByteCount, hashReusedCount, hashErrorCount int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := database.ExecContext(ctx,
		"UPDATE scans SET hash_completed_at = ?, hashed_file_count = ?, hashed_byte_count = ?, hash_reused_count = ?, hash_error_count = ? WHERE id = ?",
		now, hashedFileCount, hashedByteCount, hashReusedCount, hashErrorCount, scanID)
	return err
}

// GetHashedFileCountAndBytes returns the count and sum of sizes for files with hash_status = 'done' in the scan.
func GetHashedFileCountAndBytes(ctx context.Context, database *sql.DB, scanID int64) (fileCount, byteCount int64, err error) {
	err = database.QueryRowContext(ctx,
		"SELECT COUNT(*), COALESCE(SUM(size), 0) FROM files WHERE scan_id = ? AND hash_status = 'done'",
		scanID).Scan(&fileCount, &byteCount)
	return fileCount, byteCount, err
}

// ListScans returns all scans ordered by created_at descending (newest first).
func ListScans(ctx context.Context, database *sql.DB) ([]Scan, error) {
	return listScans(ctx, database, 0)
}

// ListScansRecent returns the most recent scans (limit rows). Use for home page dropdown to avoid loading huge tables.
func ListScansRecent(ctx context.Context, database *sql.DB, limit int) ([]Scan, error) {
	return listScans(ctx, database, limit)
}

func listScans(ctx context.Context, database *sql.DB, limit int) ([]Scan, error) {
	q := "SELECT id, created_at, completed_at, root_path, hash_started_at, hash_completed_at, file_count, scan_skipped_count, hashed_file_count, hashed_byte_count, hash_reused_count, hash_error_count FROM scans ORDER BY created_at DESC, id DESC"
	args := []interface{}{}
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := database.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var scans []Scan
	for rows.Next() {
		var id int64
		var createdAt rfc3339Time
		var completedAt, hashStartedAt, hashCompletedAt nullRFC3339Time
		var rootPath string
		var fileCount, scanSkipped, hashedFileCount, hashedByteCount, hashReused, hashError sql.NullInt64
		if err := rows.Scan(&id, &createdAt, &completedAt, &rootPath, &hashStartedAt, &hashCompletedAt, &fileCount, &scanSkipped, &hashedFileCount, &hashedByteCount, &hashReused, &hashError); err != nil {
			return nil, err
		}
		s := Scan{ID: id, CreatedAt: createdAt.Time, CompletedAt: completedAt.Ptr(), RootPath: rootPath}
		s.HashStartedAt = hashStartedAt.Ptr()
		s.HashCompletedAt = hashCompletedAt.Ptr()
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

// GetLatestIncompleteScanForRoot returns the most recent scan for rootPath that is not fully complete
// (completed_at IS NULL or hash_completed_at IS NULL). Returns 0 if none. Used to show "Continue" for a folder.
func GetLatestIncompleteScanForRoot(ctx context.Context, database *sql.DB, rootPath string) (int64, error) {
	var id int64
	err := database.QueryRowContext(ctx,
		`SELECT id FROM scans WHERE root_path = ? AND (completed_at IS NULL OR hash_completed_at IS NULL) ORDER BY created_at DESC, id DESC LIMIT 1`,
		rootPath).Scan(&id)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return id, nil
}
