package db

import (
	"context"
	"database/sql"
	"time"
)

// ScanRoot is a path configured as a scan root (folders table). Kept for API compatibility.
type ScanRoot struct {
	ID        int64
	Path      string
	CreatedAt time.Time
}

// ListScanRoots returns all folders (scan roots) ordered by id ascending.
func ListScanRoots(ctx context.Context, database *sql.DB) ([]ScanRoot, error) {
	list, err := ListFolders(ctx, database)
	if err != nil {
		return nil, err
	}
	out := make([]ScanRoot, len(list))
	for i := range list {
		out[i] = ScanRoot{ID: list[i].ID, Path: list[i].Path, CreatedAt: list[i].CreatedAt}
	}
	return out, nil
}

// AddScanRoot inserts a new folder and returns its id.
func AddScanRoot(ctx context.Context, database *sql.DB, path string) (int64, error) {
	return AddFolder(ctx, database, path)
}

// GetScanRoot returns the folder with the given id, or sql.ErrNoRows if not found.
func GetScanRoot(ctx context.Context, database *sql.DB, id int64) (*ScanRoot, error) {
	f, err := GetFolder(ctx, database, id)
	if err != nil {
		return nil, err
	}
	return &ScanRoot{ID: f.ID, Path: f.Path, CreatedAt: f.CreatedAt}, nil
}

// DeleteScanRoot removes the folder with the given id. Returns false if no row was deleted.
func DeleteScanRoot(ctx context.Context, database *sql.DB, id int64) (bool, error) {
	return DeleteFolder(ctx, database, id)
}
