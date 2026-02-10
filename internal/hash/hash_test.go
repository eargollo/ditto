package hash

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHashFile_knownContentReturnsExpectedSHA256(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	const content = "hello"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	got, err := HashFile(path)
	if err != nil {
		t.Fatalf("HashFile: %v", err)
	}
	// SHA-256 of "hello" (no newline)
	want := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if got != want {
		t.Errorf("HashFile(%q) = %q, want %q", path, got, want)
	}
}

func TestHashFile_emptyFileReturnsCorrectSHA256(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty")
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	got, err := HashFile(path)
	if err != nil {
		t.Fatalf("HashFile: %v", err)
	}
	// SHA-256 of empty input
	want := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got != want {
		t.Errorf("HashFile(empty) = %q, want %q", got, want)
	}
}

func TestHashFile_nonexistentPathReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist")

	got, err := HashFile(path)
	if err == nil {
		t.Errorf("HashFile(%q) = %q, want error", path, got)
	}
	if got != "" {
		t.Errorf("HashFile on error should return empty string, got %q", got)
	}
}
