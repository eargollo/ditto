package db

import (
	"context"
	"database/sql"
	"time"
)

// ScanRoot is a path configured as a scan root (shown in UI, used to start scans).
type ScanRoot struct {
	ID        int64
	Path      string
	CreatedAt time.Time
}

// ListScanRoots returns all scan roots ordered by id ascending.
func ListScanRoots(ctx context.Context, database *sql.DB) ([]ScanRoot, error) {
	rows, err := database.QueryContext(ctx,
		"SELECT id, path, created_at FROM scan_roots ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roots []ScanRoot
	for rows.Next() {
		var r ScanRoot
		var createdAt rfc3339Time
		if err := rows.Scan(&r.ID, &r.Path, &createdAt); err != nil {
			return nil, err
		}
		r.CreatedAt = createdAt.Time
		roots = append(roots, r)
	}
	return roots, rows.Err()
}

// AddScanRoot inserts a new scan root and returns its id.
func AddScanRoot(ctx context.Context, database *sql.DB, path string) (int64, error) {
	createdAt := time.Now().UTC().Format(time.RFC3339)
	res, err := database.ExecContext(ctx,
		"INSERT INTO scan_roots (path, created_at) VALUES (?, ?)",
		path, createdAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// DeleteScanRoot removes the scan root with the given id. Returns false if no row was deleted.
func DeleteScanRoot(ctx context.Context, database *sql.DB, id int64) (bool, error) {
	res, err := database.ExecContext(ctx, "DELETE FROM scan_roots WHERE id = ?", id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// GetScanRoot returns the scan root with the given id, or sql.ErrNoRows if not found.
func GetScanRoot(ctx context.Context, database *sql.DB, id int64) (*ScanRoot, error) {
	var r ScanRoot
	var createdAt rfc3339Time
	err := database.QueryRowContext(ctx,
		"SELECT id, path, created_at FROM scan_roots WHERE id = ?", id).
		Scan(&r.ID, &r.Path, &createdAt)
	if err != nil {
		return nil, err
	}
	r.CreatedAt = createdAt.Time
	return &r, nil
}
