package scan

import (
	"bytes"
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/eargollo/ditto/internal/db"
	"github.com/eargollo/ditto/internal/hash"
)

// baselineFixtureDir returns the path to testdata/baseline (the committed fixture for golden regression).
func baselineFixtureDir(t *testing.T) string {
	t.Helper()
	_, filename, _, _ := runtime.Caller(0)
	dir := filepath.Dir(filename)
	fixture := filepath.Join(dir, "testdata", "baseline")
	abs, err := filepath.Abs(fixture)
	if err != nil {
		t.Fatalf("resolve fixture: %v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("fixture not found at %s: %v", abs, err)
	}
	return abs
}

// filesToCSV builds CSV bytes (path, hash, size) from files. Paths are made relative to root if root != "".
func filesToCSV(files []db.File, root string) ([]byte, error) {
	type row struct {
		path string
		hash string
		size string
	}
	var rows []row
	for _, f := range files {
		path := f.Path
		if root != "" {
			rel, err := filepath.Rel(root, f.Path)
			if err != nil {
				rel = f.Path
			}
			path = filepath.ToSlash(rel)
		}
		h := ""
		if f.Hash != nil {
			h = *f.Hash
		}
		rows = append(rows, row{path, h, strconv.FormatInt(f.Size, 10)})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].path < rows[j].path })
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write([]string{"path", "hash", "size"}); err != nil {
		return nil, err
	}
	for _, r := range rows {
		if err := w.Write([]string{r.path, r.hash, r.size}); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func normalizeCSV(b []byte) string {
	return strings.ReplaceAll(strings.TrimSpace(string(b)), "\r\n", "\n")
}

// TestBaselineReferenceVsSystem runs the linear reference (Walk + hash each file) and the full
// pipeline (RunScan + RunHashPhase) on the same fixture and asserts their CSVs match.
// Proves the system matches the simple implementation on the baseline fixture.
func TestBaselineReferenceVsSystem(t *testing.T) {
	database := runTestDB(t)
	ctx := context.Background()
	fixtureRoot := baselineFixtureDir(t)

	refCSV, _, err := ReferenceCSV(ctx, fixtureRoot, nil, nil)
	if err != nil {
		t.Fatalf("ReferenceCSV: %v", err)
	}

	scanID, err := RunScan(ctx, database, fixtureRoot, nil)
	if err != nil {
		t.Fatalf("RunScan: %v", err)
	}
	if err := hash.RunHashPhase(ctx, database, scanID, &hash.HashOptions{Workers: 2}); err != nil {
		t.Fatalf("RunHashPhase: %v", err)
	}
	files, err := db.GetFilesByScanID(ctx, database, scanID)
	if err != nil {
		t.Fatalf("GetFilesByScanID: %v", err)
	}
	systemCSV, err := filesToCSV(files, "") // absolute paths to match reference
	if err != nil {
		t.Fatalf("filesToCSV: %v", err)
	}

	if normalizeCSV(refCSV) != normalizeCSV(systemCSV) {
		t.Errorf("reference CSV != system CSV.\n--- reference\n%s\n--- system\n%s", string(refCSV), string(systemCSV))
	}
}