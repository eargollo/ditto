package scan

import (
	"bufio"
	_ "embed"
	"os"
	"path/filepath"
	"strings"
)

// DefaultExcludeFileName is the name of the exclude file looked for in the scan root (like .gitignore).
const DefaultExcludeFileName = ".dittoignore"

//go:embed default.dittoignore
var defaultExcludeContent string

// DefaultExcludePatterns returns the exclude patterns from the embedded default.dittoignore (always applied).
func DefaultExcludePatterns() []string {
	return parsePatternsFromContent(defaultExcludeContent)
}

func parsePatternsFromContent(content string) []string {
	var patterns []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns
}

// LoadExcludeFile reads path and returns exclude patterns (one per non-empty line).
// Lines starting with # are comments and skipped. Leading/trailing whitespace is trimmed.
// If the file does not exist, returns nil, nil. On read error returns nil, err.
func LoadExcludeFile(path string) ([]string, error) {
	f, err := os.Open(path) // #nosec G304 -- path from config; operator-controlled
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var patterns []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return patterns, nil
}

// ExcludeFileInRoot returns the path to the default exclude file inside root (e.g. root/.dittoignore).
// Callers can pass this to LoadExcludeFile.
func ExcludeFileInRoot(root string) string {
	return filepath.Join(filepath.Clean(root), DefaultExcludeFileName)
}

// OptionsForRoot returns ScanOptions with ExcludePatterns = default patterns (from embedded default.dittoignore)
// merged with root/.dittoignore if that file exists. Other fields (e.g. MaxFilesPerSecond) are left at zero.
func OptionsForRoot(root string) (*ScanOptions, error) {
	patterns := DefaultExcludePatterns()
	path := ExcludeFileInRoot(root)
	rootPatterns, err := LoadExcludeFile(path)
	if err != nil {
		return nil, err
	}
	if len(rootPatterns) > 0 {
		patterns = append(patterns, rootPatterns...)
	}
	return &ScanOptions{ExcludePatterns: patterns}, nil
}
