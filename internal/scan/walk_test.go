package scan

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWalk_emptyDirYieldsNothing(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	var count int
	err := Walk(ctx, dir, func(e Entry) error {
		count++
		return nil
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if count != 0 {
		t.Errorf("got %d entries, want 0", count)
	}
}

func TestWalk_yieldsOnlyRegularFilesSkipsSymlinks(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// Regular file
	regPath := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(regPath, []byte("hello"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Symlink (do not yield)
	linkPath := filepath.Join(dir, "link")
	if err := os.Symlink("file.txt", linkPath); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	var entries []Entry
	err := Walk(ctx, dir, func(e Entry) error {
		entries = append(entries, e)
		return nil
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1 (symlink must be skipped)", len(entries))
	}
	e := entries[0]
	if e.Path != regPath {
		t.Errorf("Path = %q, want %q", e.Path, regPath)
	}
	if e.Size != 5 {
		t.Errorf("Size = %d, want 5", e.Size)
	}
	if e.Inode == 0 && e.DeviceID == 0 {
		t.Log("Inode/DeviceID are 0 (may be unsupported on this OS)")
	}
}

func TestWalk_nestedDirsYieldsRegularFilesAtAnyDepth(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	sub := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "root.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("write root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "deep.txt"), []byte("y"), 0644); err != nil {
		t.Fatalf("write deep: %v", err)
	}

	var paths []string
	err := Walk(ctx, dir, func(e Entry) error {
		paths = append(paths, e.Path)
		return nil
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("got %d entries, want 2", len(paths))
	}
	got := make(map[string]bool)
	for _, p := range paths {
		got[p] = true
	}
	if !got[filepath.Join(dir, "root.txt")] || !got[filepath.Join(sub, "deep.txt")] {
		t.Errorf("paths = %v", paths)
	}
}
