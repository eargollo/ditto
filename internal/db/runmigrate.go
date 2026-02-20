package db

import (
	"database/sql"
	"embed"
	"fmt"
	"log"
	"regexp"
	"sync"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// MigrationProgress holds current migration progress for logging/status.
// Updated by the migration logger as migrations run.
type MigrationProgress struct {
	mu sync.Mutex
	// Current is the migration version/name currently running (e.g. "000001_initial").
	Current string
	// Done is the number of migrations completed this run.
	Done int
	// Total is the total number of pending migrations (set when run starts).
	Total int
}

func (p *MigrationProgress) String() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Total <= 0 {
		return p.Current
	}
	return fmt.Sprintf("%s (%d/%d)", p.Current, p.Done, p.Total)
}

// progressLogger implements migrate.Logger and logs each message plus updates progress.
type progressLogger struct {
	progress *MigrationProgress
	// match migration version from log lines like "Applying migration 000001_initial.up.sql"
	versionRe *regexp.Regexp
}

func (l *progressLogger) Verbose() bool { return true }

func (l *progressLogger) Printf(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	log.Printf("db: migrate: %s", msg)
	// Update progress when we see a migration file name
	if l.versionRe != nil && l.progress != nil {
		if m := l.versionRe.FindStringSubmatch(msg); len(m) >= 2 {
			l.progress.mu.Lock()
			l.progress.Current = m[1]
			l.progress.mu.Unlock()
		}
	}
}

// RunMigrations runs pending migrations using golang-migrate (versioned, embedded SQL).
// It blocks until all migrations complete or an error occurs. Progress is logged and
// optionally exposed via MigrationProgress. Caller should set lock_timeout on the
// connection if desired (e.g. SET lock_timeout = '300s') before calling.
func RunMigrations(db *sql.DB, progress *MigrationProgress) error {
	if progress == nil {
		progress = &MigrationProgress{}
	}
	sourceDriver, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("migrate source: %w", err)
	}
	pgDriver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("migrate postgres driver: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", sourceDriver, "postgres", pgDriver)
	if err != nil {
		return fmt.Errorf("migrate new: %w", err)
	}
	defer func() { _, _ = m.Close() }()

	logger := &progressLogger{
		progress:  progress,
		versionRe: regexp.MustCompile(`(?i)(\d+_[^.]+)\.(up|down)\.sql`),
	}
	m.Log = logger

	log.Printf("db: running migrations (versioned)")
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}
	if err == migrate.ErrNoChange {
		log.Printf("db: migrations already up to date")
		return nil
	}
	progress.mu.Lock()
	progress.Current = ""
	progress.mu.Unlock()
	log.Printf("db: migrations completed")
	return nil
}
