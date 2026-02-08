package scan

import (
	"path/filepath"
	"strings"
)

// ShouldExclude reports whether path should be excluded by any of the patterns.
// If patterns is nil or empty, returns false.
// Pattern format:
//   - If pattern contains '*' or '?', it is treated as a glob matched against the path's base name (e.g. "*.log", "*.tmp").
//   - Otherwise it is a path segment: any path that has that segment as a component is excluded (e.g. ".git", "node_modules").
func ShouldExclude(path string, patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}
	base := filepath.Base(path)
	normalized := filepath.ToSlash(path)
	for _, p := range patterns {
		if strings.ContainsAny(p, "*?") {
			matched, _ := filepath.Match(p, base)
			if matched {
				return true
			}
		} else {
			if segmentMatches(normalized, p) {
				return true
			}
		}
	}
	return false
}

// segmentMatches reports whether path has segment as a path component.
func segmentMatches(path, segment string) bool {
	if path == segment {
		return true
	}
	sep := "/"
	if strings.HasSuffix(path, sep+segment) {
		return true
	}
	return strings.Contains(path, sep+segment+sep)
}
