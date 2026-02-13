package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"time"
)

// Folder is a path configured as a scan root (folders table).
// Exposed as ScanRoot in the API for compatibility.
type Folder struct {
	ID        int64
	Path      string
	CreatedAt time.Time
}

// ListFolders returns all folders (scan roots) ordered by id ascending.
func ListFolders(ctx context.Context, database *sql.DB) ([]Folder, error) {
	rows, err := database.QueryContext(ctx,
		"SELECT id, path, created_at FROM folders ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Folder
	for rows.Next() {
		var f Folder
		var createdAt time.Time
		if err := rows.Scan(&f.ID, &f.Path, &createdAt); err != nil {
			return nil, err
		}
		f.CreatedAt = createdAt
		list = append(list, f)
	}
	return list, rows.Err()
}

// AddFolder inserts a new folder and returns its id. Path is normalized to absolute before storing.
func AddFolder(ctx context.Context, database *sql.DB, path string) (int64, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return 0, err
	}
	path = filepath.Clean(path)
	var id int64
	err = database.QueryRowContext(ctx,
		"INSERT INTO folders (path, created_at) VALUES ($1, $2) RETURNING id",
		path, NowUTC()).Scan(&id)
	return id, err
}

// GetFolder returns the folder with the given id, or sql.ErrNoRows if not found.
func GetFolder(ctx context.Context, database *sql.DB, id int64) (*Folder, error) {
	var f Folder
	err := database.QueryRowContext(ctx,
		"SELECT id, path, created_at FROM folders WHERE id = $1", id).
		Scan(&f.ID, &f.Path, &f.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// DeleteFolder removes the folder with the given id. Returns false if no row was deleted.
func DeleteFolder(ctx context.Context, database *sql.DB, id int64) (bool, error) {
	res, err := database.ExecContext(ctx, "DELETE FROM folders WHERE id = $1", id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// GetOrCreateFolderByPath returns the folder id for the given path, creating the folder if it does not exist.
// Path is normalized to absolute before lookup and when creating, so folders are always stored by absolute path.
func GetOrCreateFolderByPath(ctx context.Context, database *sql.DB, path string) (int64, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return 0, err
	}
	path = filepath.Clean(path)
	var id int64
	err = database.QueryRowContext(ctx, "SELECT id FROM folders WHERE path = $1", path).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	return AddFolder(ctx, database, path)
}
