package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/eargollo/ditto/internal/config"
	"github.com/eargollo/ditto/internal/db"
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

	if err := db.Migrate(database); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	log.Print("Migrations OK")
}
