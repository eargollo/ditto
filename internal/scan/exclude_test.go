package scan

import "testing"

func TestShouldExclude_noPatternsNeverExcludes(t *testing.T) {
	if ShouldExclude("/any/path", nil) {
		t.Error("ShouldExclude with nil patterns should be false")
	}
	if ShouldExclude("/any/path", []string{}) {
		t.Error("ShouldExclude with empty patterns should be false")
	}
}

func TestShouldExclude_segmentMatchesPathComponent(t *testing.T) {
	patterns := []string{".git", "node_modules"}
	if !ShouldExclude("/repo/.git/config", patterns) {
		t.Error("path under .git should be excluded")
	}
	if !ShouldExclude("/repo/node_modules/foo", patterns) {
		t.Error("path under node_modules should be excluded")
	}
	if !ShouldExclude("/repo/.git", patterns) {
		t.Error(".git dir itself should be excluded")
	}
	if ShouldExclude("/repo/file.txt", patterns) {
		t.Error("unrelated path should not be excluded")
	}
	if ShouldExclude("/repo/.gitignore", patterns) {
		t.Error(".gitignore is not .git, should not be excluded")
	}
}

func TestShouldExclude_globMatchesBasename(t *testing.T) {
	patterns := []string{"*.log", "*.tmp"}
	if !ShouldExclude("/tmp/foo.log", patterns) {
		t.Error("*.log should match foo.log")
	}
	if !ShouldExclude("/a/b/c.tmp", patterns) {
		t.Error("*.tmp should match c.tmp")
	}
	if ShouldExclude("/tmp/foo.txt", patterns) {
		t.Error("*.log and *.tmp should not match foo.txt")
	}
}
