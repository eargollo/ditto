package scan

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadExcludeFile_notExist(t *testing.T) {
	patterns, err := LoadExcludeFile(filepath.Join(t.TempDir(), "nonexistent"))
	if err != nil {
		t.Fatalf("LoadExcludeFile: %v", err)
	}
	if patterns != nil {
		t.Errorf("LoadExcludeFile(nonexistent) = %v, want nil", patterns)
	}
}

func TestLoadExcludeFile_emptyAndComments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "excludes")
	if err := os.WriteFile(path, []byte("\n# comment\n  \n*.log\n# another\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	patterns, err := LoadExcludeFile(path)
	if err != nil {
		t.Fatalf("LoadExcludeFile: %v", err)
	}
	if len(patterns) != 1 || patterns[0] != "*.log" {
		t.Errorf("LoadExcludeFile = %v, want [*.log]", patterns)
	}
}

func TestExcludeFileInRoot(t *testing.T) {
	got := ExcludeFileInRoot("/foo/bar")
	want := "/foo/bar/.dittoignore"
	if got != want {
		t.Errorf("ExcludeFileInRoot = %q, want %q", got, want)
	}
}

func TestOptionsForRoot_noFile(t *testing.T) {
	opts, err := OptionsForRoot(t.TempDir())
	if err != nil {
		t.Fatalf("OptionsForRoot: %v", err)
	}
	if opts == nil || len(opts.ExcludePatterns) == 0 {
		t.Fatalf("OptionsForRoot(no .dittoignore) = %v, want default patterns", opts)
	}
	// Default patterns (e.g. .Encrypted) are always applied.
	if !contains(opts.ExcludePatterns, ".Encrypted") {
		t.Errorf("OptionsForRoot patterns = %v, want to include .Encrypted", opts.ExcludePatterns)
	}
}

func contains(s []string, x string) bool {
	for _, v := range s {
		if v == x {
			return true
		}
	}
	return false
}

func TestOptionsForRoot_withFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, DefaultExcludeFileName), []byte(".git\nnode_modules\n*.tmp\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	opts, err := OptionsForRoot(dir)
	if err != nil {
		t.Fatalf("OptionsForRoot: %v", err)
	}
	// Default patterns + root file patterns (e.g. .Encrypted + .git, node_modules, *.tmp).
	if opts == nil || len(opts.ExcludePatterns) < 4 {
		t.Errorf("OptionsForRoot = %+v, want at least 4 patterns (default + .git, node_modules, *.tmp)", opts)
	}
	for _, p := range []string{".Encrypted", ".git", "node_modules", "*.tmp"} {
		if !contains(opts.ExcludePatterns, p) {
			t.Errorf("OptionsForRoot patterns = %v, want to include %q", opts.ExcludePatterns, p)
		}
	}
}
