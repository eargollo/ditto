package db

import (
	"context"
	"database/sql"
	"time"
)

// Scan is a single scan run (metadata only; file records are in files table).
type Scan struct {
	ID          int64
	CreatedAt   time.Time
	CompletedAt *time.Time
	RootPath    string
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
func GetScan(ctx context.Context, db *sql.DB, id int64) (*Scan, error) {
	var rowID int64
	var createdAt rfc3339Time
	var completedAt nullRFC3339Time
	var rootPath string
	err := db.QueryRowContext(ctx,
		"SELECT id, created_at, completed_at, root_path FROM scans WHERE id = ?", id).
		Scan(&rowID, &createdAt, &completedAt, &rootPath)
	if err != nil {
		return nil, err
	}
	return &Scan{ID: rowID, CreatedAt: createdAt.Time, CompletedAt: completedAt.Ptr(), RootPath: rootPath}, nil
}

// ListScans returns all scans ordered by created_at descending (newest first).
func ListScans(ctx context.Context, db *sql.DB) ([]Scan, error) {
	rows, err := db.QueryContext(ctx,
		"SELECT id, created_at, completed_at, root_path FROM scans ORDER BY created_at DESC, id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var scans []Scan
	for rows.Next() {
		var id int64
		var createdAt rfc3339Time
		var completedAt nullRFC3339Time
		var rootPath string
		if err := rows.Scan(&id, &createdAt, &completedAt, &rootPath); err != nil {
			return nil, err
		}
		s := Scan{ID: id, CreatedAt: createdAt.Time, CompletedAt: completedAt.Ptr(), RootPath: rootPath}
		scans = append(scans, s)
	}
	return scans, rows.Err()
}
