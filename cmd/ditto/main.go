package main

import (
	"context"
	"database/sql"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/eargollo/ditto/internal/config"
	"github.com/eargollo/ditto/internal/db"
	"github.com/eargollo/ditto/internal/hash"
	"github.com/eargollo/ditto/internal/server"
	"github.com/eargollo/ditto/internal/scan"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	dataDir := cfg.DataDir()
	if err := os.MkdirAll(dataDir, 0750); err != nil {
		log.Fatalf("create data dir %q: %v", dataDir, err)
	}

	database, err := db.OpenPostgres(cfg.DatabaseURL())
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer database.Close()

	if err := db.MigratePostgres(database); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	if len(os.Args) >= 3 && os.Args[1] == "scan" {
		runScan(context.Background(), database, os.Args[2])
		return
	}

	// Single DB; Postgres handles concurrent readers and writers.
	srv, err := server.NewServer(cfg, database)
	if err != nil {
		log.Fatalf("server: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		cancel()
	}()
	log.Printf("Web UI at http://localhost:%d", cfg.Port())
	if err := srv.Run(ctx); err != nil {
		log.Fatalf("server: %v", err)
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
