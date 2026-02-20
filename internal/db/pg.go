package db

import (
	"database/sql"
	"log"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// OpenPostgres opens a PostgreSQL database using the given URL (e.g. from DATABASE_URL).
// Caller must call Close() when done. RunMigrations should be called after open to apply schema.
func OpenPostgres(url string) (*sql.DB, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		log.Printf("db: sql.Open failed: %v", err)
		return nil, err
	}
	if err := db.Ping(); err != nil {
		log.Printf("db: ping failed: %v", err)
		_ = db.Close()
		return nil, err
	}
	// Allow concurrent readers and writers; no need for a separate read-only pool.
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	// Recycle connections so we don't use ones closed by the server (proxies, managed Postgres, firewalls).
	// Order matters: set ConnMaxLifetime before ConnMaxIdleTime.
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(1 * time.Minute)
	return db, nil
}

// NowUTC returns current UTC time for use in queries (Postgres timestamptz).
func NowUTC() time.Time {
	return time.Now().UTC()
}
