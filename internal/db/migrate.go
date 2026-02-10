package db

import (
	"database/sql"
	"strings"
)

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
	// Index for duplicate/UI queries that only read done rows (avoids touching hot pending/hashing rows during scan).
	if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_files_scan_hash_status ON files(scan_id, hash_status)"); err != nil {
		return err
	}
	// Covering index for duplicate-by-hash across multiple scans (home "All" and single-scan duplicate list).
	if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_files_scan_status_hash ON files(scan_id, hash_status, hash)"); err != nil {
		return err
	}
	// HashForInode: find same-scan file with same inode that already has a hash (hardlink reuse).
	if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_files_scan_inode_device ON files(scan_id, inode, device_id)"); err != nil {
		return err
	}
	// HashForInodeFromPreviousScan: find any scan's file with same inode/device/size and hash (unchanged-file reuse).
	if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_files_inode_device_size ON files(inode, device_id, size)"); err != nil {
		return err
	}

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS scan_roots (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		path TEXT NOT NULL,
		created_at TEXT NOT NULL
	)`); err != nil {
		return err
	}

	// Hash-phase metrics (Step 4b); idempotent: ignore duplicate column
	for _, q := range []string{
		"ALTER TABLE scans ADD COLUMN hash_started_at TEXT",
		"ALTER TABLE scans ADD COLUMN hash_completed_at TEXT",
		"ALTER TABLE scans ADD COLUMN hashed_file_count INTEGER",
		"ALTER TABLE scans ADD COLUMN hashed_byte_count INTEGER",
		"ALTER TABLE scans ADD COLUMN file_count INTEGER",
		"ALTER TABLE scans ADD COLUMN scan_skipped_count INTEGER",
		"ALTER TABLE scans ADD COLUMN hash_reused_count INTEGER",
		"ALTER TABLE scans ADD COLUMN hash_error_count INTEGER",
	} {
		if _, err := db.Exec(q); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}

	// Unique (scan_id, path): one file row per path per scan (avoids duplicates when user continues scan).
	// Dedupe existing rows first, then create the index (idempotent: index already exists is fine).
	if _, err := db.Exec(`DELETE FROM files WHERE id IN (
		SELECT id FROM files EXCEPT SELECT MIN(id) FROM files GROUP BY scan_id, path
	)`); err != nil {
		return err
	}
	if _, err := db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_files_scan_id_path ON files(scan_id, path)"); err != nil {
		return err
	}

	return nil
}
