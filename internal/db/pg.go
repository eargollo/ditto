package db

import (
	"database/sql"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// OpenPostgres opens a PostgreSQL database using the given URL (e.g. from DATABASE_URL).
// Caller must call Close() when done. MigratePostgres should be called after open to create schema.
func OpenPostgres(url string) (*sql.DB, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	// Allow concurrent readers and writers; no need for a separate read-only pool.
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	return db, nil
}

// MigratePostgres creates the folders, files, scans, and file_scan tables and indexes if they do not exist.
// Idempotent; safe to call on every startup.
func MigratePostgres(db *sql.DB) error {
	// Use timestamptz for all timestamps; store in UTC.
	ddl := []string{
		`CREATE TABLE IF NOT EXISTS folders (
			id BIGSERIAL PRIMARY KEY,
			path TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC')
		)`,
		`CREATE TABLE IF NOT EXISTS scans (
			id BIGSERIAL PRIMARY KEY,
			folder_id BIGINT NOT NULL REFERENCES folders(id),
			started_at TIMESTAMPTZ NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
			completed_at TIMESTAMPTZ,
			hash_started_at TIMESTAMPTZ,
			hash_completed_at TIMESTAMPTZ,
			file_count BIGINT,
			scan_skipped_count BIGINT,
			hashed_file_count BIGINT,
			hashed_byte_count BIGINT,
			hash_reused_count BIGINT,
			hash_error_count BIGINT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_scans_folder_id ON scans(folder_id)`,
		`CREATE INDEX IF NOT EXISTS idx_scans_started_at ON scans(started_at DESC)`,
		`CREATE TABLE IF NOT EXISTS files (
			id BIGSERIAL PRIMARY KEY,
			folder_id BIGINT NOT NULL REFERENCES folders(id),
			path TEXT NOT NULL,
			size BIGINT NOT NULL,
			mtime BIGINT NOT NULL,
			inode BIGINT NOT NULL,
			device_id BIGINT,
			hash TEXT,
			hash_status TEXT NOT NULL DEFAULT 'pending',
			hashed_at TIMESTAMPTZ,
			UNIQUE(folder_id, path)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_files_folder_id ON files(folder_id)`,
		`CREATE INDEX IF NOT EXISTS idx_files_folder_id_path ON files(folder_id, path)`,
		`CREATE INDEX IF NOT EXISTS idx_files_hash_status ON files(hash_status)`,
		`CREATE INDEX IF NOT EXISTS idx_files_hash ON files(hash) WHERE hash IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_files_inode_device ON files(inode, device_id)`,
		`CREATE INDEX IF NOT EXISTS idx_files_inode_device_size ON files(inode, device_id, size)`,
		`CREATE TABLE IF NOT EXISTS file_scan (
			file_id BIGINT NOT NULL REFERENCES files(id) ON DELETE CASCADE,
			scan_id BIGINT NOT NULL REFERENCES scans(id) ON DELETE CASCADE,
			PRIMARY KEY (file_id, scan_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_file_scan_scan_id ON file_scan(scan_id)`,
		`CREATE INDEX IF NOT EXISTS idx_file_scan_file_id ON file_scan(file_id)`,
	}
	for _, q := range ddl {
		if _, err := db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

// NowUTC returns current UTC time for use in queries (Postgres timestamptz).
func NowUTC() time.Time {
	return time.Now().UTC()
}
