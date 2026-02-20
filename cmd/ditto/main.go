package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/eargollo/ditto/internal/config"
	"github.com/eargollo/ditto/internal/db"
	"github.com/eargollo/ditto/internal/hash"
	"github.com/eargollo/ditto/internal/server"
	"github.com/eargollo/ditto/internal/scan"
	"github.com/eargollo/ditto/internal/version"
)

func main() {
	// Reference mode: serial, in-memory, no DB. Never loads config or DATABASE_URL.
	if len(os.Args) >= 2 && os.Args[1] == "reference" {
		runReference()
		return
	}

	// Migrate only: open DB, run migrations, exit. Uses DITTO_TEST_DATABASE_URL if set (for tests), else DATABASE_URL via config.
	if len(os.Args) >= 2 && os.Args[1] == "migrate" {
		runMigrate()
		return
	}

	ver := version.Version
	if ver == "" {
		ver = "dev"
	}
	log.Printf("Ditto starting version=%s", ver)

	log.Printf("init: loading config")
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("init: config: %v", err)
	}
	log.Printf("init: config loaded data_dir=%s port=%d", cfg.DataDir(), cfg.Port())

	log.Printf("init: creating data directory %s", cfg.DataDir())
	dataDir := cfg.DataDir()
	if err := os.MkdirAll(dataDir, 0750); err != nil {
		log.Fatalf("init: create data dir %q: %v", dataDir, err)
	}

	log.Printf("init: connecting to database")
	database, err := openDBWithRetry(cfg.DatabaseURL(), 5, 2*time.Second)
	if err != nil {
		log.Fatalf("init: open database: %v", err)
	}
	defer database.Close()
	log.Printf("init: database connected")

	log.Printf("init: running migrations")
	if err := db.MigratePostgres(database); err != nil {
		log.Fatalf("init: migrate: %v", err)
	}
	log.Printf("init: migrations done")

	log.Printf("init: running backfill (deleted_at)")
	if err := db.BackfillFilesDeletedAt(context.Background(), database); err != nil {
		log.Printf("init: backfill deleted_at failed (non-fatal): %v", err)
	}
	log.Printf("init: backfill done")

	if len(os.Args) >= 3 && os.Args[1] == "scan" {
		runScan(context.Background(), database, os.Args[2])
		return
	}

	log.Printf("init: building HTTP server")
	srv, err := server.NewServer(cfg, database)
	if err != nil {
		log.Fatalf("init: NewServer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		cancel()
	}()
	log.Printf("init: starting HTTP server on port %d", cfg.Port())
	log.Printf("Web UI at http://localhost:%d", cfg.Port())
	if err := srv.Run(ctx); err != nil {
		log.Fatalf("init: server Run: %v", err)
	}
}

func runReference() {
	refFlags := flag.NewFlagSet("reference", flag.ExitOnError)
	outPath := refFlags.String("o", "", "output CSV path (default stdout)")
	statsPath := refFlags.String("stats", "", "output stats CSV path (same fields as DB scan row)")
	_ = refFlags.Parse(os.Args[2:])
	args := refFlags.Args()
	if len(args) < 1 {
		log.Fatal("usage: ditto reference [-o output.csv] [-stats stats.csv] <root>\n  (no DATABASE_URL or server required)")
	}
	root := filepath.Clean(args[0])
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		if err != nil {
			log.Fatalf("reference: %v", err)
		}
		log.Fatalf("reference: %s is not a directory", root)
	}
	opts, err := scan.OptionsForRoot(root)
	if err != nil {
		log.Fatalf("reference exclude: %v", err)
	}
	log.Printf("[reference] walking %s (serial, in-memory, only hash duplicate candidates)", root)
	progress := func(phase string, current, total int64) {
		if total < 0 {
			log.Printf("[reference] %s: %d files", phase, current)
		} else {
			log.Printf("[reference] %s: %d/%d files (%.0f%%)", phase, current, total, 100*float64(current)/float64(total))
		}
	}
	csvBytes, refStats, err := scan.ReferenceCSV(context.Background(), root, opts.ExcludePatterns, progress)
	if err != nil {
		log.Fatalf("reference: %v", err)
	}
	if refStats != nil {
		log.Printf("[reference] files=%d skipped=%d hashed=%d hashed_bytes=%d reused=%d errors=%d walk_sec=%.1f hash_sec=%.1f",
			refStats.FileCount, refStats.ScanSkippedCount, refStats.HashedFileCount, refStats.HashedByteCount,
			refStats.HashReusedCount, refStats.HashErrorCount, refStats.WalkDurationSec, refStats.HashDurationSec)
	}
	if *outPath == "" {
		_, _ = os.Stdout.Write(csvBytes)
	} else {
		if err := os.WriteFile(*outPath, csvBytes, 0600); err != nil {
			log.Fatalf("reference write: %v", err)
		}
		log.Printf("[reference] wrote %d bytes to %s", len(csvBytes), *outPath)
	}
	if *statsPath != "" && refStats != nil {
		var buf bytes.Buffer
		w := csv.NewWriter(&buf)
		_ = w.Write([]string{"file_count", "scan_skipped_count", "hashed_file_count", "hashed_byte_count", "hash_reused_count", "hash_error_count", "walk_duration_sec", "hash_duration_sec"})
		_ = w.Write([]string{
			strconv.FormatInt(refStats.FileCount, 10),
			strconv.FormatInt(refStats.ScanSkippedCount, 10),
			strconv.FormatInt(refStats.HashedFileCount, 10),
			strconv.FormatInt(refStats.HashedByteCount, 10),
			strconv.FormatInt(refStats.HashReusedCount, 10),
			strconv.FormatInt(refStats.HashErrorCount, 10),
			strconv.FormatFloat(refStats.WalkDurationSec, 'f', -1, 64),
			strconv.FormatFloat(refStats.HashDurationSec, 'f', -1, 64),
		})
		w.Flush()
		if err := os.WriteFile(*statsPath, buf.Bytes(), 0600); err != nil {
			log.Fatalf("reference stats write: %v", err)
		}
		log.Printf("[reference] wrote stats to %s", *statsPath)
	}
}

func runScan(ctx context.Context, database *sql.DB, rootPath string) {
	opts, err := scan.OptionsForRoot(rootPath)
	if err != nil {
		log.Fatalf("exclude file: %v", err)
	}
	scanID, err := scan.RunScan(ctx, database, rootPath, opts)
	if err != nil {
		log.Fatalf("scan: %v", err)
	}
	log.Printf("Scan complete: id=%d", scanID)

	if err := hash.RunHashPhase(ctx, database, scanID, &hash.HashOptions{Workers: 6}); err != nil {
		log.Fatalf("hash phase: %v", err)
	}
	log.Printf("Hash phase complete for scan %d. Use the Web UI to view duplicates.", scanID)
}

// openDBWithRetry opens the database, retrying up to attempts times with interval between tries.
// Use this at server startup so the app can wait for Postgres to become ready (e.g. in Docker).
func openDBWithRetry(url string, attempts int, interval time.Duration) (*sql.DB, error) {
	var lastErr error
	for i := 0; i < attempts; i++ {
		conn, err := db.OpenPostgres(url)
		if err == nil {
			return conn, nil
		}
		lastErr = err
		if i < attempts-1 {
			log.Printf("init: database connect attempt %d/%d failed: %v; retrying in %v", i+1, attempts, err, interval)
			time.Sleep(interval)
		}
	}
	log.Printf("init: database connect failed after %d attempts: %v", attempts, lastErr)
	return nil, lastErr
}

func runMigrate() {
	log.Print("migrate: starting")
	url := os.Getenv("DITTO_TEST_DATABASE_URL")
	if url == "" {
		log.Print("migrate: loading config for DATABASE_URL")
		cfg, err := config.Load()
		if err != nil {
			log.Fatalf("migrate: config: %v", err)
		}
		url = cfg.DatabaseURL()
	}
	log.Print("migrate: opening database")
	conn, err := db.OpenPostgres(url)
	if err != nil {
		log.Fatalf("migrate: open database: %v", err)
	}
	defer conn.Close()
	log.Print("migrate: running migrations")
	if err := db.MigratePostgres(conn); err != nil {
		log.Fatalf("migrate: run migrations: %v", err)
	}
	log.Print("migrate: done")
}
