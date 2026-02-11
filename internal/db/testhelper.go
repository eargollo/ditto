package db

import (
	"database/sql"
	"os"
	"testing"
)

// TestPostgresDB opens a PostgreSQL connection from DATABASE_URL, runs MigratePostgres, and truncates tables so each test gets a clean state.
// DATABASE_URL must be set (e.g. postgres://ditto:ditto@localhost:5432/ditto?sslmode=disable). Run tests with -p 1 to avoid cross-package truncate deadlocks.
func TestPostgresDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Fatal("DATABASE_URL is required for tests (e.g. postgres://ditto:ditto@localhost:5432/ditto?sslmode=disable)")
	}
	db, err := OpenPostgres(url)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := MigratePostgres(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Clean state so tests see predictable data. Run tests with -p 1 to avoid cross-package truncate deadlocks.
	for _, table := range []string{"file_scan", "files", "scans", "folders"} {
		if _, err := db.Exec("TRUNCATE TABLE " + table + " RESTART IDENTITY CASCADE"); err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}
	return db
}
