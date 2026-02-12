package db

import (
	"database/sql"
	"os"
	"strings"
	"testing"
)

// DefaultTestDatabaseURL is the default PostgreSQL URL for tests when DATABASE_URL is unset.
// Matches docker-compose.dev.yml (postgres service: ditto/ditto@localhost:5432/ditto).
// Credentials are for local/dev test DB only, not production.
//
// #nosec G101 -- test default for local Postgres (docker-compose.dev.yml), not a production secret
const DefaultTestDatabaseURL = "postgres://ditto:ditto@localhost:5432/ditto?sslmode=disable"

// TestPostgresDB opens a PostgreSQL connection from DATABASE_URL (or DefaultTestDatabaseURL if unset), runs MigratePostgres, and truncates tables so each test gets a clean state.
// With docker compose -f docker-compose.dev.yml up -d, tests work without setting DATABASE_URL. Run tests with -p 1 to avoid cross-package truncate deadlocks.
func TestPostgresDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = DefaultTestDatabaseURL
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
			msg := err.Error()
			if strings.Contains(msg, "deadlock") || strings.Contains(msg, "40P01") {
				msg = msg + " — run go test -p 1 ./... or make test to avoid cross-package deadlocks"
			}
			t.Fatalf("truncate %s: %s", table, msg)
		}
	}
	return db
}
