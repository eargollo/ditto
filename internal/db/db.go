package db

import (
	"database/sql"

	_ "modernc.org/sqlite"
)

// Open opens a SQLite database at path and enables WAL mode for better
// write throughput (ADR-002). The caller must call db.Close() when done.
// For in-memory DB use path ":memory:".
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}

	_, err = db.Exec("PRAGMA journal_mode=WAL")
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}
