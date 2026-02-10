package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
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
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Fatalf("create data dir %q: %v", dataDir, err)
	}

	dbPath := filepath.Join(dataDir, "ditto.db")
	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("open db %q: %v", dbPath, err)
	}
	defer database.Close()

	// Read-only connection so the UI stays responsive during scans (WAL allows concurrent readers).
	readDB, err := db.OpenReadOnly(dbPath)
	if err != nil {
		log.Fatalf("open read-only db: %v", err)
	}
	if readDB != nil {
		defer readDB.Close()
	}

	if err := db.Migrate(database); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	if len(os.Args) >= 3 && os.Args[1] == "scan" {
		runScan(context.Background(), database, os.Args[2])
		return
	}

	srv, err := server.NewServer(cfg, database, readDB)
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
	log.Printf("Hash phase complete for scan %d", scanID)

	// ADR-007: duplicate and current-state queries are scoped to this scan (latest snapshot).
	rows, err := database.QueryContext(ctx,
		`SELECT size, COUNT(*) as cnt FROM files WHERE scan_id = ? GROUP BY size ORDER BY cnt DESC, size DESC`,
		scanID)
	if err != nil {
		log.Fatalf("query: %v", err)
	}
	defer rows.Close()

	type sizeCount struct {
		size  int64
		count int
	}
	var sizes []sizeCount
	for rows.Next() {
		var sc sizeCount
		if err := rows.Scan(&sc.size, &sc.count); err != nil {
			log.Fatalf("scan row: %v", err)
		}
		sizes = append(sizes, sc)
	}
	if err := rows.Err(); err != nil {
		log.Fatalf("rows: %v", err)
	}

	// Paths only for duplicate candidates (count > 1), scoped to this scan_id.
	pathRows, err := database.QueryContext(ctx,
		`SELECT size, path FROM files WHERE scan_id = ? AND size IN (
			SELECT size FROM files WHERE scan_id = ? GROUP BY size HAVING COUNT(*) > 1
		) ORDER BY size, path`,
		scanID, scanID)
	if err != nil {
		log.Fatalf("query paths: %v", err)
	}
	defer pathRows.Close()
	pathsBySize := make(map[int64][]string)
	for pathRows.Next() {
		var size int64
		var path string
		if err := pathRows.Scan(&size, &path); err != nil {
			log.Fatalf("scan path: %v", err)
		}
		pathsBySize[size] = append(pathsBySize[size], path)
	}
	if err := pathRows.Err(); err != nil {
		log.Fatalf("path rows: %v", err)
	}

	fmt.Println("\n--- Files by size (duplicate candidates: count > 1) ---")
	fmt.Printf("%-16s %s\n", "size", "count")
	fmt.Printf("%-16s %s\n", "----", "-----")
	for _, sc := range sizes {
		fmt.Printf("%-16d %d\n", sc.size, sc.count)
		for _, path := range pathsBySize[sc.size] {
			fmt.Printf("  %s\n", path)
		}
	}
}
