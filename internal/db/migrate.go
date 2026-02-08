package db

import "database/sql"

// Migrate creates the scans and files tables if they do not exist, and enables
// foreign keys. Idempotent; safe to call on every startup.
func Migrate(db *sql.DB) error {
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return err
	}

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS scans (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		created_at TEXT NOT NULL,
		completed_at TEXT,
		root_path TEXT
	)`); err != nil {
		return err
	}

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS files (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		scan_id INTEGER NOT NULL REFERENCES scans(id),
		path TEXT NOT NULL,
		size INTEGER NOT NULL,
		mtime INTEGER NOT NULL,
		inode INTEGER NOT NULL,
		device_id INTEGER,
		hash TEXT,
		hash_status TEXT NOT NULL DEFAULT 'pending',
		hashed_at TEXT
	)`); err != nil {
		return err
	}

	if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_files_scan_id ON files(scan_id)"); err != nil {
		return err
	}
	if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_files_scan_id_size ON files(scan_id, size)"); err != nil {
		return err
	}

	return nil
}
