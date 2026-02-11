package hash

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/eargollo/ditto/internal/db"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	return db.TestPostgresDB(t)
}

// addFileToScan inserts a file into the scan (folder from dir, relative path from absPath).
func addFileToScan(ctx context.Context, database *sql.DB, dir string, scanID int64, absPath string, size, mtime, inode int64, deviceID *int64) {
	folderID, _ := db.GetOrCreateFolderByPath(ctx, database, dir)
	rel, _ := filepath.Rel(dir, absPath)
	if rel == "" || rel == "." {
		rel = filepath.Base(absPath)
	}
	fileID, _ := db.UpsertFile(ctx, database, folderID, rel, size, mtime, inode, deviceID)
	_ = db.InsertFileScan(ctx, database, fileID, scanID)
}

func TestRunHashPhase_fillsHashForDuplicateCandidatesOnly(t *testing.T) {
	database := testDB(t)
	ctx := context.Background()
	dir := t.TempDir()

	folderID, _ := db.GetOrCreateFolderByPath(ctx, database, dir)
	scan, _ := db.CreateScan(ctx, database, folderID)
	path1 := filepath.Join(dir, "a.txt")
	path2 := filepath.Join(dir, "b.txt")
	path3 := filepath.Join(dir, "c.txt")
	for _, p := range []string{path1, path2, path3} {
		if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	abs1, _ := filepath.Abs(path1)
	abs2, _ := filepath.Abs(path2)
	abs3, _ := filepath.Abs(path3)
	addFileToScan(ctx, database, dir, scan.ID, abs1, 100, 1, 1, nil)
	addFileToScan(ctx, database, dir, scan.ID, abs2, 100, 2, 2, nil)
	addFileToScan(ctx, database, dir, scan.ID, abs3, 200, 3, 3, nil)
	if err := db.UpdateScanCompletedAt(ctx, database, scan.ID, 3, 0); err != nil {
		t.Fatalf("UpdateScanCompletedAt: %v", err)
	}

	if err := RunHashPhase(ctx, database, scan.ID, nil); err != nil {
		t.Fatalf("RunHashPhase: %v", err)
	}

	files, err := db.GetFilesByScanID(ctx, database, scan.ID)
	if err != nil {
		t.Fatalf("GetFilesByScanID: %v", err)
	}
	byPath := make(map[string]db.File)
	for _, f := range files {
		byPath[f.Path] = f
	}
	// Path in byPath may be full (dir/a.txt) or absolute
	for _, path := range []string{abs1, abs2, filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")} {
		if f, ok := byPath[path]; ok {
			if f.Hash == nil || f.HashStatus != "done" || f.HashedAt == nil {
				t.Errorf("file %s: hash=%v status=%q hashed_at=%v", path, f.Hash, f.HashStatus, f.HashedAt)
			}
		}
	}
	var f3 *db.File
	for i := range files {
		if filepath.Base(files[i].Path) == "c.txt" {
			f3 = &files[i]
			break
		}
	}
	if f3 == nil {
		t.Fatal("file c.txt not found in results")
	}
	if f3.Hash != nil || f3.HashStatus != "pending" {
		t.Errorf("unique size file should remain pending: hash=%v status=%q", f3.Hash, f3.HashStatus)
	}
}

func TestRunHashPhase_twoFilesSameSizeSameHash(t *testing.T) {
	database := testDB(t)
	ctx := context.Background()
	dir := t.TempDir()

	folderID, _ := db.GetOrCreateFolderByPath(ctx, database, dir)
	scan, _ := db.CreateScan(ctx, database, folderID)
	content := []byte("identical")
	path1 := filepath.Join(dir, "a.txt")
	path2 := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(path1, content, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(path2, content, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	abs1, _ := filepath.Abs(path1)
	abs2, _ := filepath.Abs(path2)
	addFileToScan(ctx, database, dir, scan.ID, abs1, int64(len(content)), 1, 1, nil)
	addFileToScan(ctx, database, dir, scan.ID, abs2, int64(len(content)), 2, 2, nil)
	db.UpdateScanCompletedAt(ctx, database, scan.ID, 2, 0)

	if err := RunHashPhase(ctx, database, scan.ID, nil); err != nil {
		t.Fatalf("RunHashPhase: %v", err)
	}

	files, _ := db.GetFilesByScanID(ctx, database, scan.ID)
	if len(files) != 2 {
		t.Fatalf("want 2 files, got %d", len(files))
	}
	h1, h2 := files[0].Hash, files[1].Hash
	if h1 == nil || h2 == nil || *h1 != *h2 {
		t.Errorf("same content should have same hash: %v vs %v", h1, h2)
	}
}

func TestRunHashPhase_setsHashMetricsOnScan(t *testing.T) {
	database := testDB(t)
	ctx := context.Background()
	dir := t.TempDir()

	folderID, _ := db.GetOrCreateFolderByPath(ctx, database, dir)
	scan, _ := db.CreateScan(ctx, database, folderID)
	path1 := filepath.Join(dir, "a.txt")
	path2 := filepath.Join(dir, "b.txt")
	os.WriteFile(path1, []byte("x"), 0644)
	os.WriteFile(path2, []byte("x"), 0644)
	abs1, _ := filepath.Abs(path1)
	abs2, _ := filepath.Abs(path2)
	addFileToScan(ctx, database, dir, scan.ID, abs1, 1, 1, 1, nil)
	addFileToScan(ctx, database, dir, scan.ID, abs2, 1, 2, 2, nil)
	db.UpdateScanCompletedAt(ctx, database, scan.ID, 2, 0)

	if err := RunHashPhase(ctx, database, scan.ID, nil); err != nil {
		t.Fatalf("RunHashPhase: %v", err)
	}

	s, err := db.GetScan(ctx, database, scan.ID)
	if err != nil {
		t.Fatalf("GetScan: %v", err)
	}
	if s.HashStartedAt == nil || s.HashCompletedAt == nil {
		t.Errorf("hash_started_at or hash_completed_at not set: %+v", s)
	}
	if s.HashCompletedAt != nil && s.HashStartedAt != nil && s.HashCompletedAt.Before(*s.HashStartedAt) {
		t.Error("hash_completed_at should be >= hash_started_at")
	}
	if s.HashedFileCount == nil || *s.HashedFileCount != 2 {
		t.Errorf("hashed_file_count = %v, want 2", s.HashedFileCount)
	}
	if s.HashedByteCount == nil || *s.HashedByteCount != 2 {
		t.Errorf("hashed_byte_count = %v, want 2", s.HashedByteCount)
	}
}

func TestRunHashPhase_noMetricsWhenNotRun(t *testing.T) {
	database := testDB(t)
	ctx := context.Background()
	folderID, _ := db.AddFolder(ctx, database, "/tmp")
	scan, _ := db.CreateScan(ctx, database, folderID)
	s, _ := db.GetScan(ctx, database, scan.ID)
	if s.HashStartedAt != nil || s.HashCompletedAt != nil || s.HashedFileCount != nil || s.HashedByteCount != nil {
		t.Errorf("scan without hash phase should have null metrics: %+v", s)
	}
}

func TestRunHashPhase_hardlinkReuseOneRead(t *testing.T) {
	database := testDB(t)
	ctx := context.Background()
	dir := t.TempDir()

	folderID, _ := db.GetOrCreateFolderByPath(ctx, database, dir)
	scan, _ := db.CreateScan(ctx, database, folderID)
	path1 := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path1, []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	path2 := filepath.Join(dir, "b.txt")
	if err := os.Link(path1, path2); err != nil {
		t.Skipf("hardlink not supported: %v", err)
	}
	abs1, _ := filepath.Abs(path1)
	abs2, _ := filepath.Abs(path2)
	info1, _ := os.Stat(path1)
	info2, _ := os.Stat(path2)
	inode1 := inodeOf(info1)
	inode2 := inodeOf(info2)
	dev := deviceOf(info1)
	addFileToScan(ctx, database, dir, scan.ID, abs1, 1, 1, inode1, dev)
	addFileToScan(ctx, database, dir, scan.ID, abs2, 1, 2, inode2, dev)
	db.UpdateScanCompletedAt(ctx, database, scan.ID, 2, 0)

	if err := RunHashPhase(ctx, database, scan.ID, nil); err != nil {
		t.Fatalf("RunHashPhase: %v", err)
	}

	files, _ := db.GetFilesByScanID(ctx, database, scan.ID)
	if len(files) != 2 {
		t.Fatalf("want 2 files, got %d", len(files))
	}
	if files[0].Hash == nil || files[1].Hash == nil || *files[0].Hash != *files[1].Hash {
		t.Errorf("hardlinks should have same hash: %v vs %v", files[0].Hash, files[1].Hash)
	}
}

func inodeOf(info os.FileInfo) int64 {
	if info == nil {
		return 0
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return int64(st.Ino)
	}
	return 0
}

func deviceOf(info os.FileInfo) *int64 {
	if info == nil {
		return nil
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		d := int64(st.Dev)
		return &d
	}
	return nil
}

func TestRunHashPhase_twoWorkersAllFilesHashedOnce(t *testing.T) {
	database := testDB(t)
	ctx := context.Background()
	dir := t.TempDir()

	folderID, _ := db.GetOrCreateFolderByPath(ctx, database, dir)
	scan, _ := db.CreateScan(ctx, database, folderID)
	for i := 0; i < 6; i++ {
		path := filepath.Join(dir, fmt.Sprintf("f%d.txt", i))
		if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
		abs, _ := filepath.Abs(path)
		size := int64(10 + (i / 2))
		addFileToScan(ctx, database, dir, scan.ID, abs, size, int64(i), int64(i+1), nil)
	}
	db.UpdateScanCompletedAt(ctx, database, scan.ID, 6, 0)

	if err := RunHashPhase(ctx, database, scan.ID, &HashOptions{Workers: 2}); err != nil {
		t.Fatalf("RunHashPhase: %v", err)
	}

	files, _ := db.GetFilesByScanID(ctx, database, scan.ID)
	done := 0
	for _, f := range files {
		if f.HashStatus == "done" {
			done++
			if f.Hash == nil {
				t.Errorf("file id=%d done but hash nil", f.ID)
			}
		}
	}
	if done != 6 {
		t.Errorf("want 6 files hashed, got %d", done)
	}
}

func TestRunHashPhase_throttleEnabledDelays(t *testing.T) {
	database := testDB(t)
	ctx := context.Background()
	dir := t.TempDir()

	folderID, _ := db.GetOrCreateFolderByPath(ctx, database, dir)
	scan, _ := db.CreateScan(ctx, database, folderID)
	for i := 0; i < 3; i++ {
		path := filepath.Join(dir, fmt.Sprintf("f%d.txt", i))
		os.WriteFile(path, []byte("x"), 0644)
		abs, _ := filepath.Abs(path)
		addFileToScan(ctx, database, dir, scan.ID, abs, 1, int64(i), int64(i+1), nil)
	}
	db.UpdateScanCompletedAt(ctx, database, scan.ID, 3, 0)

	start := time.Now()
	if err := RunHashPhase(ctx, database, scan.ID, &HashOptions{MaxHashesPerSecond: 5}); err != nil {
		t.Fatalf("RunHashPhase: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 400*time.Millisecond {
		t.Errorf("throttle 5/s with 3 files: elapsed %v, want >= 400ms", elapsed)
	}
}

func TestRunHashPhase_contextCancellationStopsLoop(t *testing.T) {
	database := testDB(t)
	ctx := context.Background()
	dir := t.TempDir()
	folderID, _ := db.GetOrCreateFolderByPath(ctx, database, dir)
	scan, _ := db.CreateScan(ctx, database, folderID)
	for i := 0; i < 10; i++ {
		path := filepath.Join(dir, fmt.Sprintf("f%d.txt", i))
		os.WriteFile(path, []byte("x"), 0644)
		abs, _ := filepath.Abs(path)
		addFileToScan(ctx, database, dir, scan.ID, abs, 1, int64(i), int64(i+1), nil)
	}
	db.UpdateScanCompletedAt(ctx, database, scan.ID, 10, 0)

	ctx, cancel := context.WithCancel(ctx)
	cancel() // cancel immediately so RunHashPhase exits quickly
	err := RunHashPhase(ctx, database, scan.ID, nil)
	if err != context.Canceled {
		t.Errorf("RunHashPhase with canceled ctx: err = %v, want context.Canceled", err)
	}
}

func TestRunHashPhase_throttleDisabledFast(t *testing.T) {
	database := testDB(t)
	ctx := context.Background()
	dir := t.TempDir()

	folderID, _ := db.GetOrCreateFolderByPath(ctx, database, dir)
	scan, _ := db.CreateScan(ctx, database, folderID)
	for i := 0; i < 5; i++ {
		path := filepath.Join(dir, fmt.Sprintf("f%d.txt", i))
		os.WriteFile(path, []byte("x"), 0644)
		abs, _ := filepath.Abs(path)
		addFileToScan(ctx, database, dir, scan.ID, abs, 1, int64(i), int64(i+1), nil)
	}
	db.UpdateScanCompletedAt(ctx, database, scan.ID, 5, 0)

	start := time.Now()
	if err := RunHashPhase(ctx, database, scan.ID, &HashOptions{MaxHashesPerSecond: 0}); err != nil {
		t.Fatalf("RunHashPhase: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed > 500*time.Millisecond {
		t.Errorf("throttle disabled: elapsed %v, want < 500ms", elapsed)
	}
}
