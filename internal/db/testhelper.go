package db

import (
	"context"
	"os"
	"testing"
)

// truncateTestTables clears tables used by tests so each test sees a clean slate within its tx.
// Call only from TestPostgresDB (inside the same tx); rollback at test end restores prior state.
func truncateTestTables(ctx context.Context, q Querier) error {
	_, err := q.ExecContext(ctx, `TRUNCATE TABLE duplicate_groups_hash, file_scan, files, scans, folders RESTART IDENTITY CASCADE`)
	return err
}

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

// TestPostgresDB opens PostgreSQL (TestDatabaseURL), starts a transaction, truncates test tables so the test
// sees a clean slate, and returns a Database (backed by that tx). Cleanup rolls back the tx and closes the
// connection, so each test is isolated and tests can run in parallel across packages.
// Schema must already exist (run "make migrate" before tests).
// For integration tests use a real *sql.DB and truncate (e.g. internal/integration testDB).
func TestPostgresDB(t *testing.T) Database {
	t.Helper()
	url := TestDatabaseURL()
	conn, err := OpenPostgres(url)
	if err != nil {
		t.Fatalf("open postgres: %v (tests require Postgres; start with: docker compose -f docker-compose.dev.yml up -d; then: make migrate). url: %s", err, url)
	}
	t.Cleanup(func() { _ = conn.Close() })
	tx, err := conn.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	// Truncate so this test sees empty tables; rollback at end restores prior state.
	if err := truncateTestTables(context.Background(), tx); err != nil {
		t.Fatalf("truncate test tables: %v", err)
	}
	return &txDB{tx}
}
