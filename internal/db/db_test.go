package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpen_enablesWAL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open() err = %v", err)
	}
	defer db.Close()

	var mode string
	err = db.QueryRow("PRAGMA journal_mode").Scan(&mode)
	if err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
}

func TestOpen_createsDBFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "created.db")

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open() err = %v", err)
	}
	db.Close()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("DB file %q was not created", path)
	}

	db2, err := Open(path)
	if err != nil {
		t.Fatalf("Open() second time: %v", err)
	}
	defer db2.Close()

	var n int
	err = db2.QueryRow("SELECT 1").Scan(&n)
	if err != nil {
		t.Fatalf("Query after reopen: %v", err)
	}
	if n != 1 {
		t.Errorf("SELECT 1 = %d, want 1", n)
	}
}

func TestClose_preventsUse(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open() err = %v", err)
	}
	db.Close()

	err = db.Ping()
	if err == nil {
		t.Error("Ping() after Close() succeeded, want error")
	}
}
