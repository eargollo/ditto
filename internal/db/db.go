package db

import (
	"database/sql"
	"path/filepath"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

// busyTimeoutMS is how long SQLite waits (ms) before returning SQLITE_BUSY when locked.
// Applied per-connection via DSN so all pool connections get it (multi-worker hash + HTTP handlers).
const busyTimeoutMS = 30000 // 30 seconds

// readOnlyBusyTimeoutMS is used for the read-only connection pool. In WAL mode readers
// don't block on writers, so this is a fallback; keep it short so UI doesn't hang.
const readOnlyBusyTimeoutMS = 5000 // 5 seconds

// Open opens a SQLite database at path and enables WAL mode for better
// write throughput (ADR-002). The caller must call db.Close() when done.
// For in-memory DB use path ":memory:". With ":memory:", the URI form
// file::memory:?cache=shared is used so all connections in the pool share
// the same database (otherwise each connection gets its own empty DB).
func Open(path string) (*sql.DB, error) {
	dsn := path
	if path == ":memory:" {
		dsn = "file::memory:?cache=shared&_busy_timeout=" + strconv.Itoa(busyTimeoutMS)
	} else {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		dsn = path + sep + "_busy_timeout=" + strconv.Itoa(busyTimeoutMS)
	}
	db, err := sql.Open("sqlite", dsn)
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

// OpenReadOnly opens a read-only SQLite connection to the same database file.
// In WAL mode, readers don't block on writers, so read-heavy handlers (e.g. home page)
// stay responsive while the hash phase is writing. Returns (nil, nil) for ":memory:".
// Caller should call Close() when done; use this for a separate read-only pool.
func OpenReadOnly(path string) (*sql.DB, error) {
	if path == ":memory:" {
		return nil, nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	// URI with mode=ro; forward slashes for SQLite URI
	uri := "file:" + filepath.ToSlash(abs) + "?mode=ro&_busy_timeout=" + strconv.Itoa(readOnlyBusyTimeoutMS)
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}
