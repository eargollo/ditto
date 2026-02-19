package db

import (
	"context"
	"os"
	"testing"
)

// DefaultTestDatabaseURL is the default PostgreSQL URL for tests.
// Uses the ditto_test database so tests and development (make run, which uses ditto) stay separate.
// Matches docker-compose.dev.yml (postgres service with ditto + ditto_test).
// Credentials are for local/dev test DB only, not production.
//
// #nosec G101 -- test default for local Postgres (docker-compose.dev.yml), not a production secret
const DefaultTestDatabaseURL = "postgres://ditto:ditto@localhost:5432/ditto_test?sslmode=disable"

// TestDatabaseURL returns the DB URL for tests: DITTO_TEST_DATABASE_URL if set, else DefaultTestDatabaseURL.
// Tests never use DATABASE_URL so that go test ./... defaults to the test DB even when DATABASE_URL points at dev.
func TestDatabaseURL() string {
	if url := os.Getenv("DITTO_TEST_DATABASE_URL"); url != "" {
		return url
	}
	return DefaultTestDatabaseURL
}

// TestPostgresDB opens PostgreSQL (TestDatabaseURL), starts a transaction, and returns a Database (backed by that tx).
// Schema must already exist (run "ditto migrate" with DITTO_TEST_DATABASE_URL before tests). Cleanup rolls back the tx
// and closes the connection, so each test is isolated and tests can run in parallel.
// For integration tests use a real *sql.DB and truncate (e.g. internal/integration testDB).
func TestPostgresDB(t *testing.T) Database {
	t.Helper()
	url := TestDatabaseURL()
	conn, err := OpenPostgres(url)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	tx, err := conn.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	return &txDB{tx}
}
