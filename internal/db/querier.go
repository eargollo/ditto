package db

import (
	"context"
	"database/sql"
)

// Querier runs Exec/Query/QueryRow. Both *sql.DB and *sql.Tx implement it.
// Used so tests can pass a *sql.Tx (rollback for isolation) and production passes *sql.DB.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Database is a Querier that can be pinged (e.g. health checks). *sql.DB implements it.
// For unit tests we use txDB (*sql.Tx wrapper with no-op Ping).
type Database interface {
	Querier
	PingContext(ctx context.Context) error
}

// txDB wraps *sql.Tx so it implements Database (PingContext is a no-op for a transaction).
type txDB struct{ *sql.Tx }

func (t txDB) PingContext(context.Context) error { return nil }
