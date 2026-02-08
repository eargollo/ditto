package scan

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eargollo/ditto/internal/db"
)

func runTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return database
}

func TestRunScan_populatesScanAndFilesWithCompletedAt(t *testing.T) {
	database := runTestDB(t)
	ctx := context.Background()
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("y"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	scanID, err := RunScan(ctx, database, dir, nil)
	if err != nil {
		t.Fatalf("RunScan: %v", err)
	}
	if scanID <= 0 {
		t.Errorf("scanID = %d, want > 0", scanID)
	}

	s, err := db.GetScan(ctx, database, scanID)
	if err != nil {
		t.Fatalf("GetScan: %v", err)
	}
	if s.CompletedAt == nil {
		t.Error("CompletedAt should be set")
	}
	if s.RootPath != dir {
		t.Errorf("RootPath = %q, want %q", s.RootPath, dir)
	}

	files, err := db.GetFilesByScanID(ctx, database, scanID)
	if err != nil {
		t.Fatalf("GetFilesByScanID: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2", len(files))
	}
	paths := make(map[string]bool)
	for _, f := range files {
		paths[f.Path] = true
	}
	if !paths[filepath.Join(dir, "a.txt")] || !paths[filepath.Join(dir, "b.txt")] {
		t.Errorf("files = %v", files)
	}
}

func TestRunScan_withExcludesReducesFileCount(t *testing.T) {
	database := runTestDB(t)
	ctx := context.Background()
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skip.log"), []byte("y"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	opts := &ScanOptions{ExcludePatterns: []string{"*.log"}}
	scanID, err := RunScan(ctx, database, dir, opts)
	if err != nil {
		t.Fatalf("RunScan: %v", err)
	}

	files, err := db.GetFilesByScanID(ctx, database, scanID)
	if err != nil {
		t.Fatalf("GetFilesByScanID: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	if files[0].Path != filepath.Join(dir, "keep.txt") {
		t.Errorf("file path = %q", files[0].Path)
	}
}

func TestRunScan_nonexistentRootReturnsErrorNoScanRow(t *testing.T) {
	database := runTestDB(t)
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "does-not-exist")

	_, err := RunScan(ctx, database, root, nil)
	if err == nil {
		t.Fatal("RunScan: want error for nonexistent root")
	}

	scans, err := db.ListScans(ctx, database)
	if err != nil {
		t.Fatalf("ListScans: %v", err)
	}
	if len(scans) != 0 {
		t.Errorf("expected no scan row, got %d", len(scans))
	}
}

func TestRunScan_throttleDisabledIsFast(t *testing.T) {
	database := runTestDB(t)
	ctx := context.Background()
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%d.txt", i)), []byte("x"), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	start := time.Now()
	_, err := RunScan(ctx, database, dir, &ScanOptions{MaxFilesPerSecond: 0})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("RunScan: %v", err)
	}
	// With throttle disabled, 5 files should complete in well under 100ms on any reasonable machine.
	if elapsed > 100*time.Millisecond {
		t.Errorf("throttle disabled: elapsed %v, want < 100ms (full speed)", elapsed)
	}
}

func TestRunScan_throttleEnabledDelays(t *testing.T) {
	database := runTestDB(t)
	ctx := context.Background()
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%d.txt", i)), []byte("x"), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	// 10 files/s => 100ms between files. After first file we have 2 waits (before 2nd and 3rd) => at least ~200ms.
	start := time.Now()
	_, err := RunScan(ctx, database, dir, &ScanOptions{MaxFilesPerSecond: 10})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("RunScan: %v", err)
	}
	minElapsed := 150 * time.Millisecond // generous margin
	if elapsed < minElapsed {
		t.Errorf("throttle 10/s with 3 files: elapsed %v, want >= %v", elapsed, minElapsed)
	}
}
