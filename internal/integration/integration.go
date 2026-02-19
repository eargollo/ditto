//go:build integration

// Package integration provides end-to-end integration tests for scan → hash → duplicate grouping.
// Tests run the system as a black box: the real "ditto scan" CLI is executed as a subprocess against
// the same Postgres and temp dir as the test. Assertions use the DB only to verify outcomes (see docs/plan/integration-regression-tests.md).
package integration
