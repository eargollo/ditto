//go:build integration

package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eargollo/ditto/internal/config"
	"github.com/eargollo/ditto/internal/db"
	"github.com/eargollo/ditto/internal/server"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	url := databaseURL(t)
	conn, err := db.OpenPostgres(url)
	if err != nil {
		t.Fatalf("open postgres: %v (integration tests require Postgres; start with: docker compose -f docker-compose.dev.yml up -d; then make migrate)", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	// Schema must exist (run "make migrate" before integration tests).
	if err := truncateTables(conn); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	t.Cleanup(func() { _ = truncateTables(conn) })
	return conn
}

func truncateTables(database *sql.DB) error {
	tables := []string{"duplicate_groups_hash", "file_scan", "files", "scans", "folders"}
	for _, table := range tables {
		if _, err := database.Exec("TRUNCATE TABLE " + table + " RESTART IDENTITY CASCADE"); err != nil {
			return fmt.Errorf("truncate %s: %w", table, err)
		}
	}
	return nil
}

func databaseURL(t *testing.T) string {
	t.Helper()
	return db.TestDatabaseURL()
}

// startService starts the real server the same way main does (config, DB, Run), with port 0
// so the kernel picks a port. Returns the base URL (e.g. http://127.0.0.1:12345) and the DB.
// All interaction with the app must go through HTTP (same surface as the UX).
func startService(t *testing.T) (baseURL string, database *sql.DB) {
	t.Helper()
	// Force test DB for the server (tests never use DATABASE_URL from env so dev DB is not touched).
	os.Setenv(config.EnvDatabaseURL, databaseURL(t))
	os.Setenv(config.EnvPort, "0")
	database = testDB(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if cfg.Port() != 0 {
		t.Fatalf("expected DITTO_PORT=0 for tests, got %d", cfg.Port())
	}
	srv, err := server.NewServer(cfg, database)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		_ = srv.Run(ctx)
	}()
	<-srv.ListenReady()
	baseURL = srv.ListenAddr()
	if baseURL == "" {
		t.Fatal("server did not set listen address (expected when DITTO_PORT=0)")
	}
	return baseURL, database
}

// makeFixtureDir creates a temp dir and writes named files with the given content. Returns the dir path.
func makeFixtureDir(t *testing.T, files map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, content, 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

// runScanAndHash starts a scan via the API (same as UX: POST /api/scans) and polls status until
// hash is complete. Returns the scan ID. Exercises exactly the same surface as the user.
func runScanAndHash(ctx context.Context, t *testing.T, baseURL, dir string) int64 {
	t.Helper()
	body, err := json.Marshal(map[string]string{"root_path": dir})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/scans", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/scans: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /api/scans: status = %d, want 202", resp.StatusCode)
	}
	var createResp struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&createResp); err != nil {
		t.Fatalf("decode POST /api/scans response: %v", err)
	}
	scanID := createResp.ID
	if scanID == 0 {
		t.Fatal("POST /api/scans returned id 0")
	}

	// Poll status until hash_completed_at is set (same as UX progress page).
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			t.Fatalf("context done while waiting for scan %d", scanID)
		default:
		}
		statusReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/scans/%d/status", baseURL, scanID), nil)
		statusResp, err := http.DefaultClient.Do(statusReq)
		if err != nil {
			t.Fatalf("GET /api/scans/%d/status: %v", scanID, err)
		}
		var status struct {
			HashCompletedAt *string `json:"hash_completed_at,omitempty"`
		}
		_ = json.NewDecoder(statusResp.Body).Decode(&status)
		statusResp.Body.Close()
		if status.HashCompletedAt != nil && *status.HashCompletedAt != "" {
			return scanID
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("scan %d did not complete hash phase within 2 minutes", scanID)
	return 0
}

// API response shapes used for assertions (same surface as UX).
type apiSummary struct {
	GroupCount      int64 `json:"group_count"`
	TotalFiles      int64 `json:"total_files"`
	TotalSize       int64 `json:"total_size"`
	ReclaimableSize int64 `json:"reclaimable_size"`
}

// apiGroupsResponse is the response from GET /api/duplicates/groups.
type apiGroupsResponse struct {
	Groups []apiDuplicateGroup `json:"groups"`
	Total  int64               `json:"total"`
}

type apiDuplicateGroup struct {
	Hash            string           `json:"hash"`
	FileCount       int64            `json:"file_count"`
	TotalSize       int64            `json:"total_size"`
	ReclaimableSize int64            `json:"reclaimable_size"`
	Files           []apiFileInGroup `json:"files,omitempty"`
}

type apiFileInGroup struct {
	ID   int64  `json:"id"`
	Path string `json:"path"`
}

type apiScanStatus struct {
	ID              int64   `json:"id"`
	FileCount       *int64  `json:"file_count,omitempty"`
	HashedFileCount *int64  `json:"hashed_file_count,omitempty"`
	HashReusedCount *int64  `json:"hash_reused_count,omitempty"`
	HashedReadCount int64   `json:"hashed_read_count"`
	HashErrorCount  *int64  `json:"hash_error_count,omitempty"`
}

func getSummary(t *testing.T, baseURL string) apiSummary {
	t.Helper()
	resp, err := http.Get(baseURL + "/api/duplicates/summary")
	if err != nil {
		t.Fatalf("GET /api/duplicates/summary: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/duplicates/summary: status = %d", resp.StatusCode)
	}
	var s apiSummary
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	return s
}

func getScanStatus(t *testing.T, baseURL string, scanID int64) apiScanStatus {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("%s/api/scans/%d/status", baseURL, scanID))
	if err != nil {
		t.Fatalf("GET /api/scans/%d/status: %v", scanID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/scans/%d/status: status = %d", resp.StatusCode, scanID)
	}
	var s apiScanStatus
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	return s
}

func assertGrouping(t *testing.T, baseURL string, wantGroupCount, wantTotalFiles, wantReclaimable int64) {
	t.Helper()
	s := getSummary(t, baseURL)
	if s.GroupCount != wantGroupCount {
		t.Errorf("group count = %d, want %d", s.GroupCount, wantGroupCount)
	}
	if s.TotalFiles != wantTotalFiles {
		t.Errorf("total files in groups = %d, want %d", s.TotalFiles, wantTotalFiles)
	}
	if s.ReclaimableSize != wantReclaimable {
		t.Errorf("reclaimable size = %d, want %d", s.ReclaimableSize, wantReclaimable)
	}
}

// getGroups fetches duplicate groups from the API (limit 50, max 50 files per group). Used to obtain a group's hash and file IDs for refresh/delete tests.
func getGroups(t *testing.T, baseURL string) apiGroupsResponse {
	t.Helper()
	resp, err := http.Get(baseURL + "/api/duplicates/groups?limit=50&offset=0&max_files_per_group=50")
	if err != nil {
		t.Fatalf("GET /api/duplicates/groups: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/duplicates/groups: status = %d", resp.StatusCode)
	}
	var r apiGroupsResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		t.Fatalf("decode groups: %v", err)
	}
	return r
}

// refreshGroup calls POST /api/duplicates/groups/refresh with the given hash. Fails the test on non-2xx.
func refreshGroup(t *testing.T, baseURL, hash string) {
	t.Helper()
	body, err := json.Marshal(map[string]string{"hash": hash})
	if err != nil {
		t.Fatalf("marshal refresh body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/duplicates/groups/refresh", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/duplicates/groups/refresh: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/duplicates/groups/refresh: status = %d, want 200", resp.StatusCode)
	}
}

// deleteFile calls POST /api/duplicates/files/delete with the given file_id. Returns status code and error body if non-2xx.
func deleteFile(t *testing.T, baseURL string, fileID int64) (statusCode int, errMsg string) {
	t.Helper()
	body, err := json.Marshal(map[string]int64{"file_id": fileID})
	if err != nil {
		t.Fatalf("marshal delete body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/duplicates/files/delete", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/duplicates/files/delete: %v", err)
	}
	defer resp.Body.Close()
	var errBody struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&errBody)
	return resp.StatusCode, errBody.Error
}

func assertScanStats(t *testing.T, baseURL string, scanID int64, wantFileCount, wantHashed, wantReused, wantRead, wantHashErrors int64) {
	t.Helper()
	s := getScanStatus(t, baseURL, scanID)
	fc := int64(0)
	if s.FileCount != nil {
		fc = *s.FileCount
	}
	hashed := int64(0)
	if s.HashedFileCount != nil {
		hashed = *s.HashedFileCount
	}
	reused := int64(0)
	if s.HashReusedCount != nil {
		reused = *s.HashReusedCount
	}
	errCount := int64(0)
	if s.HashErrorCount != nil {
		errCount = *s.HashErrorCount
	}
	if fc != wantFileCount {
		t.Errorf("scan %d file_count = %d, want %d", scanID, fc, wantFileCount)
	}
	if hashed != wantHashed {
		t.Errorf("scan %d hashed_file_count = %d, want %d", scanID, hashed, wantHashed)
	}
	if reused != wantReused {
		t.Errorf("scan %d hash_reused_count = %d, want %d", scanID, reused, wantReused)
	}
	if s.HashedReadCount != wantRead {
		t.Errorf("scan %d hashed_read_count = %d, want %d", scanID, s.HashedReadCount, wantRead)
	}
	if errCount != wantHashErrors {
		t.Errorf("scan %d hash_error_count = %d, want %d", scanID, errCount, wantHashErrors)
	}
}

// logScanExport fetches GET /scans/{id}/export (CSV) and logs each file (path, hash, size). Visible only with -v.
func logScanExport(t *testing.T, baseURL string, scanID int64) {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("%s/scans/%d/export", baseURL, scanID))
	if err != nil {
		t.Logf("scan %d export: %v", scanID, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Logf("scan %d export: status %d", scanID, resp.StatusCode)
		return
	}
	body, _ := io.ReadAll(resp.Body)
	r := csv.NewReader(bytes.NewReader(body))
	rows, err := r.ReadAll()
	if err != nil || len(rows) < 2 {
		t.Logf("scan %d export: parse error or empty", scanID)
		return
	}
	// rows[0] is header: path, hash, size
	t.Logf("scan %d files:", scanID)
	for _, row := range rows[1:] {
		if len(row) < 3 {
			continue
		}
		path, hashVal, sizeStr := row[0], row[1], row[2]
		if hashVal == "" {
			hashVal = "(no hash)"
		}
		t.Logf("  %s  hash=%s  size=%s", path, hashVal, sizeStr)
	}
}

// getScanExportRows returns path, hash, size rows for the scan (empty on error). hash empty string means no hash.
func getScanExportRows(t *testing.T, baseURL string, scanID int64) [][3]string {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("%s/scans/%d/export", baseURL, scanID))
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	r := csv.NewReader(bytes.NewReader(body))
	rows, err := r.ReadAll()
	if err != nil || len(rows) < 2 {
		return nil
	}
	var out [][3]string
	for _, row := range rows[1:] {
		if len(row) < 3 {
			continue
		}
		out = append(out, [3]string{row[0], row[1], row[2]})
	}
	return out
}

// logStats writes scan stats, file list with hashes, and grouping summary to the test log. Visible only with -v.
// When multiple scanIDs are given, also logs an "All files" section so you can see every file (including scan 1) and its hash status.
func logStats(t *testing.T, baseURL string, scanIDs ...int64) {
	t.Helper()
	var allRows [][][3]string
	for _, scanID := range scanIDs {
		s := getScanStatus(t, baseURL, scanID)
		fc, hashed, reused, errs := int64(0), int64(0), int64(0), int64(0)
		if s.FileCount != nil {
			fc = *s.FileCount
		}
		if s.HashedFileCount != nil {
			hashed = *s.HashedFileCount
		}
		if s.HashReusedCount != nil {
			reused = *s.HashReusedCount
		}
		if s.HashErrorCount != nil {
			errs = *s.HashErrorCount
		}
		t.Logf("scan %d stats: scanned=%d hashed=%d reused=%d read=%d hash_errors=%d",
			scanID, fc, hashed, reused, s.HashedReadCount, errs)
		rows := getScanExportRows(t, baseURL, scanID)
		allRows = append(allRows, rows)
		t.Logf("scan %d files:", scanID)
		for _, row := range rows {
			hashVal := row[1]
			if hashVal == "" {
				hashVal = "(no hash)"
			}
			t.Logf("  %s  hash=%s  size=%s", row[0], hashVal, row[2])
		}
	}
	if len(scanIDs) > 1 {
		t.Logf("all files:")
		for i, scanID := range scanIDs {
			rows := allRows[i]
			for _, row := range rows {
				hashVal := row[1]
				if hashVal == "" {
					hashVal = "(no hash)"
				}
				t.Logf("  [scan %d] %s  hash=%s  size=%s", scanID, row[0], hashVal, row[2])
			}
			if i < len(scanIDs)-1 && len(rows) > 0 {
				t.Logf("  ---")
			}
		}
	}
	sum := getSummary(t, baseURL)
	t.Logf("groups: count=%d total_files=%d reclaimable=%d",
		sum.GroupCount, sum.TotalFiles, sum.ReclaimableSize)
}

var (
	contentAB = []byte("contentAB") // A and B identical
	contentCD = []byte("contentCD") // C and D identical
	contentE  = []byte("contentE")  // E unique
)

func firstScanFixture(t *testing.T) (dir string, files map[string][]byte) {
	files = map[string][]byte{
		"A": contentAB,
		"B": contentAB,
		"C": contentCD,
		"D": contentCD,
		"E": contentE,
	}
	dir = makeFixtureDir(t, files)
	return dir, files
}

var sizeAB = int64(len(contentAB))
var sizeCD = int64(len(contentCD))

// twoFolderFixture creates folder 1 (A1, B1 same content) and folder 2 (C2, D2 same content; E2 same content as A1/B1, not in folder 1; F2 same size, unique content).
// E2 is "equal size to folder one" and equal content but was not scanned on folder 1 (it lives in folder 2).
func twoFolderFixture(t *testing.T) (dir1, dir2 string) {
	t.Helper()
	dir1 = makeFixtureDir(t, map[string][]byte{"A1": contentAB, "B1": contentAB})
	dir2 = makeFixtureDir(t, map[string][]byte{
		"C2": contentCD,            // same size as folder 1, different content; duplicate within folder 2
		"D2": contentCD,            // same as C2
		"E2": contentAB,            // same size and content as A1/B1 (cross-folder duplicate); not in folder 1
		"F2": []byte("unique!!!"),  // same size (9), unique content
	})
	return dir1, dir2
}

// Folder 1 different sizes so nothing is hashed (no size appears twice in scan 1).
// Folder 2: two files with P1's content (so both hashed, one duplicate group); one file same size as Q1 but different (not hashed in scan 2).
var (
	contentP = []byte("a")   // 1 byte
	contentQ = []byte("bb")  // 2 bytes
	contentR = []byte("a")   // same as P1
	contentS = []byte("cc")  // 2 bytes, same size as Q1, different content
)
var sizeP = int64(len(contentP)) // 1
var sizeQ = int64(len(contentQ)) // 2

// twoFolderNoDupThenMatchFixture: folder 1 has two files with different sizes → 0 hashed, 0 groups.
// Folder 2 has two files equal to P1 (R2, R2b) so they get hashed and form one group; one file (S2) same size as Q1 but different content (not hashed in scan 2).
func twoFolderNoDupThenMatchFixture(t *testing.T) (dir1, dir2 string) {
	t.Helper()
	dir1 = makeFixtureDir(t, map[string][]byte{"P1": contentP, "Q1": contentQ})
	dir2 = makeFixtureDir(t, map[string][]byte{
		"R2":  contentR, // same size and content as P1
		"R2b": contentR, // duplicate of R2 (same content as P1) → 1 group in folder 2
		"S2":  contentS, // same size as Q1 but different content; only file of this size in scan 2 so not hashed
	})
	return dir1, dir2
}

// contentTriple: three identical files for "one group, delete one/two then refresh" tests.
var contentTriple = []byte("triple")
var sizeTriple = int64(len(contentTriple))

// threeFilesOneGroupFixture creates a dir with three files sharing the same content (one duplicate group of 3).
func threeFilesOneGroupFixture(t *testing.T) string {
	t.Helper()
	return makeFixtureDir(t, map[string][]byte{
		"A": contentTriple,
		"B": contentTriple,
		"C": contentTriple,
	})
}

// contentPair: two identical files for "one group, change one then refresh" test.
var contentPair = []byte("pair!")
var sizePair = int64(len(contentPair))

// twoFilesOneGroupFixture creates a dir with two files sharing the same content (one duplicate group of 2).
func twoFilesOneGroupFixture(t *testing.T) string {
	t.Helper()
	return makeFixtureDir(t, map[string][]byte{
		"X": contentPair,
		"Y": contentPair,
	})
}

func TestIntegration_1_FirstScanWithDuplicates(t *testing.T) {
	ctx := context.Background()
	baseURL, _ := startService(t)
	dir, _ := firstScanFixture(t)

	scanID := runScanAndHash(ctx, t, baseURL, dir)
	logStats(t, baseURL, scanID)

	assertGrouping(t, baseURL, 2, 4, sizeAB+sizeCD)
	assertScanStats(t, baseURL, scanID, 5, 4, 0, 4, 0)
}

func TestIntegration_2_SecondScanNoChanges(t *testing.T) {
	ctx := context.Background()
	baseURL, _ := startService(t)
	dir, _ := firstScanFixture(t)

	scan1 := runScanAndHash(ctx, t, baseURL, dir)
	scan2 := runScanAndHash(ctx, t, baseURL, dir)
	logStats(t, baseURL, scan1, scan2)

	assertGrouping(t, baseURL, 2, 4, sizeAB+sizeCD)
	assertScanStats(t, baseURL, scan1, 5, 4, 0, 4, 0)
	assertScanStats(t, baseURL, scan2, 5, 4, 4, 0, 0)
}

func TestIntegration_3_OneDuplicateChangesSameSize(t *testing.T) {
	ctx := context.Background()
	baseURL, _ := startService(t)
	dir, _ := firstScanFixture(t)

	runScanAndHash(ctx, t, baseURL, dir)
	// Ensure we're in a new second before writing so B's mtime differs from hashed_mtime and UpdateFilesDeletedAtForScan sets B to pending.
	time.Sleep(2 * time.Second)
	if err := os.WriteFile(filepath.Join(dir, "B"), []byte("different"), 0644); err != nil {
		t.Fatalf("write B: %v", err)
	}
	scan2 := runScanAndHash(ctx, t, baseURL, dir)
	logStats(t, baseURL, scan2)

	// B content changed (same size) → B re-read; 1 group (C,D), 4 hashed, 3 reused, 1 read.
	assertGrouping(t, baseURL, 1, 2, sizeCD)
	assertScanStats(t, baseURL, scan2, 5, 4, 3, 1, 0)
}

func TestIntegration_4_OneDuplicateChangesDiffSizeUnique(t *testing.T) {
	ctx := context.Background()
	baseURL, _ := startService(t)
	dir, _ := firstScanFixture(t)

	runScanAndHash(ctx, t, baseURL, dir)
	// Ensure we're in a new second before writing B so 1s-resolution filesystems record a new mtime.
	time.Sleep(2 * time.Second)
	if err := os.WriteFile(filepath.Join(dir, "B"), []byte("x"), 0644); err != nil {
		t.Fatalf("write B: %v", err)
	}
	scan2 := runScanAndHash(ctx, t, baseURL, dir)
	logStats(t, baseURL, scan2)

	assertGrouping(t, baseURL, 1, 2, sizeCD)
	assertScanStats(t, baseURL, scan2, 5, 3, 3, 0, 0)
}

func TestIntegration_5_OneFileBecomesDuplicate(t *testing.T) {
	ctx := context.Background()
	baseURL, _ := startService(t)
	dir, _ := firstScanFixture(t)

	runScanAndHash(ctx, t, baseURL, dir)
	if err := os.WriteFile(filepath.Join(dir, "E"), contentAB, 0644); err != nil {
		t.Fatalf("write E: %v", err)
	}
	scan2 := runScanAndHash(ctx, t, baseURL, dir)
	logStats(t, baseURL, scan2)

	assertGrouping(t, baseURL, 2, 5, 27)
	assertScanStats(t, baseURL, scan2, 5, 5, 4, 1, 0)
}

func TestIntegration_6_DeleteFilesBetweenScans(t *testing.T) {
	ctx := context.Background()
	baseURL, _ := startService(t)
	dir, _ := firstScanFixture(t)

	runScanAndHash(ctx, t, baseURL, dir)
	if err := os.Remove(filepath.Join(dir, "B")); err != nil {
		t.Fatalf("remove B: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "D")); err != nil {
		t.Fatalf("remove D: %v", err)
	}
	scan2 := runScanAndHash(ctx, t, baseURL, dir)
	logStats(t, baseURL, scan2)

	assertGrouping(t, baseURL, 0, 0, 0)
	assertScanStats(t, baseURL, scan2, 3, 2, 2, 0, 0)
}

// TestIntegration_7_FileDeletedThenRecreatedWithDifferentData ensures that when a file is
// removed (gets deleted_at), then recreated with different content (same size), the next scan
// "undeletes" it and re-hashes it instead of reusing the old hash. Catches bugs where we
// would wrongly reuse a deleted file's hash for the new content.
func TestIntegration_7_FileDeletedThenRecreatedWithDifferentData(t *testing.T) {
	ctx := context.Background()
	baseURL, _ := startService(t)
	dir, _ := firstScanFixture(t)

	runScanAndHash(ctx, t, baseURL, dir)
	// Delete B (was duplicate of A). Scan again so B gets deleted_at. Still have A, C, D, E → (C,D) one group.
	if err := os.Remove(filepath.Join(dir, "B")); err != nil {
		t.Fatalf("remove B: %v", err)
	}
	scan2 := runScanAndHash(ctx, t, baseURL, dir)
	assertGrouping(t, baseURL, 1, 2, sizeCD)
	assertScanStats(t, baseURL, scan2, 4, 3, 3, 0, 0)

	// Ensure new B gets a different mtime so hash reset (mtime != hashed_mtime) runs; 1s-resolution filesystems in CI.
	time.Sleep(2 * time.Second)
	// Recreate B with different content, same size (9 bytes). Must not reuse old hash.
	contentBNew := []byte("different")
	if len(contentBNew) != len(contentAB) {
		t.Fatalf("test sanity: B new content size %d != contentAB size %d", len(contentBNew), len(contentAB))
	}
	if err := os.WriteFile(filepath.Join(dir, "B"), contentBNew, 0644); err != nil {
		t.Fatalf("write B: %v", err)
	}
	scan3 := runScanAndHash(ctx, t, baseURL, dir)
	logStats(t, baseURL, scan3)

	// Scan 3: A, B (recreated), C, D, E. 5 files. Duplicate-size: A, B, C, D. B re-read (new content). (C,D) still one group; A and B different hashes.
	assertGrouping(t, baseURL, 1, 2, sizeCD)
	assertScanStats(t, baseURL, scan3, 5, 4, 3, 1, 0)
}

func TestIntegration_8_TwoFolders_CrossFolderAndWithinFolderDuplicates(t *testing.T) {
	ctx := context.Background()
	baseURL, _ := startService(t)
	dir1, dir2 := twoFolderFixture(t)

	scan1 := runScanAndHash(ctx, t, baseURL, dir1)
	scan2 := runScanAndHash(ctx, t, baseURL, dir2)
	logStats(t, baseURL, scan1, scan2)

	// Group 1: (A1, B1, E2) same hash → reclaimable 18; group 2: (C2, D2) same hash → reclaimable 9; total 27.
	assertGrouping(t, baseURL, 2, 5, 2*sizeAB+sizeCD)
	assertScanStats(t, baseURL, scan1, 2, 2, 0, 2, 0)
	assertScanStats(t, baseURL, scan2, 4, 4, 0, 4, 0)
}

func TestIntegration_9_TwoFolders_FirstNoDupSecondMatchAndSameSizeDiff(t *testing.T) {
	ctx := context.Background()
	baseURL, _ := startService(t)
	dir1, dir2 := twoFolderNoDupThenMatchFixture(t)

	scan1 := runScanAndHash(ctx, t, baseURL, dir1)
	scan2 := runScanAndHash(ctx, t, baseURL, dir2)
	logStats(t, baseURL, scan1, scan2)

	// Scan 1: P1, Q1 different sizes → 0 hashed in scan 1 phase. When scan 2 runs, global queue hashes all: P1, Q1, R2, R2b, S2.
	// Group 1: (P1, R2, R2b) same content → reclaimable 2*sizeP = 2. Q1 and S2 same size but different content → no group.
	assertGrouping(t, baseURL, 1, 3, 2*sizeP)
	assertScanStats(t, baseURL, scan1, 2, 2, 0, 2, 0)
	assertScanStats(t, baseURL, scan2, 3, 3, 0, 3, 0)
}

// TestIntegration_10_GroupRefresh_OneDeleted: group of 3 files, delete one from disk, refresh group → group still exists with 2 files.
func TestIntegration_10_GroupRefresh_OneDeleted(t *testing.T) {
	ctx := context.Background()
	baseURL, _ := startService(t)
	dir := threeFilesOneGroupFixture(t)

	runScanAndHash(ctx, t, baseURL, dir)
	// One group of 3 files (total 3*sizeTriple, reclaimable 2*sizeTriple).
	assertGrouping(t, baseURL, 1, 3, 2*sizeTriple)

	groups := getGroups(t, baseURL)
	if len(groups.Groups) != 1 || groups.Groups[0].FileCount != 3 {
		t.Fatalf("expected one group with 3 files, got %d groups", len(groups.Groups))
	}
	hash := groups.Groups[0].Hash

	if err := os.Remove(filepath.Join(dir, "B")); err != nil {
		t.Fatalf("remove B: %v", err)
	}
	refreshGroup(t, baseURL, hash)

	// Group still exists with 2 files; reclaimable = one copy = sizeTriple.
	assertGrouping(t, baseURL, 1, 2, sizeTriple)
}

// TestIntegration_11_GroupRefresh_OneChanged: group of 2 files, change one file (same size), refresh group → group no longer exists.
func TestIntegration_11_GroupRefresh_OneChanged(t *testing.T) {
	ctx := context.Background()
	baseURL, _ := startService(t)
	dir := twoFilesOneGroupFixture(t)

	runScanAndHash(ctx, t, baseURL, dir)
	assertGrouping(t, baseURL, 1, 2, sizePair)

	groups := getGroups(t, baseURL)
	if len(groups.Groups) != 1 || groups.Groups[0].FileCount != 2 {
		t.Fatalf("expected one group with 2 files, got %d groups", len(groups.Groups))
	}
	hash := groups.Groups[0].Hash

	// Change X content (same size) so mtime/size check will flag it for rehash; refresh marks it pending so group disappears.
	time.Sleep(2 * time.Second)
	if err := os.WriteFile(filepath.Join(dir, "X"), []byte("diff!"), 0644); err != nil {
		t.Fatalf("write X: %v", err)
	}
	refreshGroup(t, baseURL, hash)

	// Only one file still has that hash → no duplicate group.
	assertGrouping(t, baseURL, 0, 0, 0)
}

// TestIntegration_12_GroupRefresh_TwoDeleted: group of 3 files, delete two from disk, refresh group → group no longer exists.
func TestIntegration_12_GroupRefresh_TwoDeleted(t *testing.T) {
	ctx := context.Background()
	baseURL, _ := startService(t)
	dir := threeFilesOneGroupFixture(t)

	runScanAndHash(ctx, t, baseURL, dir)
	assertGrouping(t, baseURL, 1, 3, 2*sizeTriple)

	groups := getGroups(t, baseURL)
	if len(groups.Groups) != 1 || groups.Groups[0].FileCount != 3 {
		t.Fatalf("expected one group with 3 files, got %d groups", len(groups.Groups))
	}
	hash := groups.Groups[0].Hash

	if err := os.Remove(filepath.Join(dir, "B")); err != nil {
		t.Fatalf("remove B: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "C")); err != nil {
		t.Fatalf("remove C: %v", err)
	}
	refreshGroup(t, baseURL, hash)

	// Only one file left → no duplicate group.
	assertGrouping(t, baseURL, 0, 0, 0)
}

// TestIntegration_13_DeleteFile_Success: two files same content, delete one via API → file gone, no duplicate group.
func TestIntegration_13_DeleteFile_Success(t *testing.T) {
	ctx := context.Background()
	baseURL, _ := startService(t)
	dir := twoFilesOneGroupFixture(t)

	runScanAndHash(ctx, t, baseURL, dir)
	assertGrouping(t, baseURL, 1, 2, sizePair)

	groups := getGroups(t, baseURL)
	if len(groups.Groups) != 1 || len(groups.Groups[0].Files) < 2 {
		t.Fatalf("expected one group with 2 files, got %d groups, %d files", len(groups.Groups), len(groups.Groups[0].Files))
	}
	fileID := groups.Groups[0].Files[0].ID
	deletedPath := groups.Groups[0].Files[0].Path

	status, _ := deleteFile(t, baseURL, fileID)
	if status != http.StatusOK {
		t.Fatalf("delete file: status = %d, want 200", status)
	}
	if _, err := os.Stat(deletedPath); err == nil {
		t.Errorf("deleted file should not exist on disk: %s", deletedPath)
	}
	assertGrouping(t, baseURL, 0, 0, 0)
}

// TestIntegration_14_DeleteFile_RejectLastInGroup: after deleting one of two, try to delete the remaining → 400.
func TestIntegration_14_DeleteFile_RejectLastInGroup(t *testing.T) {
	ctx := context.Background()
	baseURL, _ := startService(t)
	dir := twoFilesOneGroupFixture(t)

	runScanAndHash(ctx, t, baseURL, dir)
	groups := getGroups(t, baseURL)
	if len(groups.Groups) != 1 || len(groups.Groups[0].Files) < 2 {
		t.Fatalf("expected one group with 2 files")
	}
	firstID := groups.Groups[0].Files[0].ID
	secondID := groups.Groups[0].Files[1].ID

	status, _ := deleteFile(t, baseURL, firstID)
	if status != http.StatusOK {
		t.Fatalf("first delete: status = %d, want 200", status)
	}
	assertGrouping(t, baseURL, 0, 0, 0)

	status, errMsg := deleteFile(t, baseURL, secondID)
	if status != http.StatusBadRequest {
		t.Fatalf("second delete (last in group): status = %d, want 400; msg=%s", status, errMsg)
	}
	if _, err := os.Stat(groups.Groups[0].Files[1].Path); err != nil {
		t.Errorf("remaining file should still exist on disk")
	}
}

// TestIntegration_15_DeleteFile_RejectWhenFileToDeleteChanged: file to delete was modified on disk → 400.
func TestIntegration_15_DeleteFile_RejectWhenFileToDeleteChanged(t *testing.T) {
	ctx := context.Background()
	baseURL, _ := startService(t)
	dir := twoFilesOneGroupFixture(t)

	runScanAndHash(ctx, t, baseURL, dir)
	groups := getGroups(t, baseURL)
	if len(groups.Groups) != 1 || len(groups.Groups[0].Files) < 2 {
		t.Fatalf("expected one group with 2 files")
	}
	// Overwrite one file (X) with different content so re-hash will not match.
	time.Sleep(2 * time.Second)
	if err := os.WriteFile(filepath.Join(dir, "X"), []byte("changed"), 0644); err != nil {
		t.Fatalf("write X: %v", err)
	}
	// Find the file that points to X (path ends with /X or \X).
	var fileIDToDelete int64
	for _, f := range groups.Groups[0].Files {
		if filepath.Base(f.Path) == "X" {
			fileIDToDelete = f.ID
			break
		}
	}
	if fileIDToDelete == 0 {
		t.Fatal("could not find file X in group")
	}

	status, errMsg := deleteFile(t, baseURL, fileIDToDelete)
	if status != http.StatusBadRequest {
		t.Fatalf("delete changed file: status = %d, want 400; msg=%s", status, errMsg)
	}
	// File should still exist.
	if _, err := os.Stat(filepath.Join(dir, "X")); err != nil {
		t.Errorf("file X should still exist after rejected delete")
	}
}

// TestIntegration_16_DeleteFile_RejectWhenOtherFileChanged: other file in group modified → 400.
func TestIntegration_16_DeleteFile_RejectWhenOtherFileChanged(t *testing.T) {
	ctx := context.Background()
	baseURL, _ := startService(t)
	dir := twoFilesOneGroupFixture(t)

	runScanAndHash(ctx, t, baseURL, dir)
	groups := getGroups(t, baseURL)
	if len(groups.Groups) != 1 || len(groups.Groups[0].Files) < 2 {
		t.Fatalf("expected one group with 2 files")
	}
	// Overwrite Y (the "other" file) so verification of the other file fails.
	time.Sleep(2 * time.Second)
	if err := os.WriteFile(filepath.Join(dir, "Y"), []byte("changed"), 0644); err != nil {
		t.Fatalf("write Y: %v", err)
	}
	// Try to delete X (the unchanged file).
	var fileIDToDelete int64
	for _, f := range groups.Groups[0].Files {
		if filepath.Base(f.Path) == "X" {
			fileIDToDelete = f.ID
			break
		}
	}
	if fileIDToDelete == 0 {
		t.Fatal("could not find file X in group")
	}

	status, _ := deleteFile(t, baseURL, fileIDToDelete)
	if status != http.StatusBadRequest {
		t.Fatalf("delete when other changed: status = %d, want 400", status)
	}
}

// TestIntegration_17_DeleteFile_ThreeFiles_GroupRefreshed: delete one of three → group still exists with 2 files.
func TestIntegration_17_DeleteFile_ThreeFiles_GroupRefreshed(t *testing.T) {
	ctx := context.Background()
	baseURL, _ := startService(t)
	dir := threeFilesOneGroupFixture(t)

	runScanAndHash(ctx, t, baseURL, dir)
	assertGrouping(t, baseURL, 1, 3, 2*sizeTriple)

	groups := getGroups(t, baseURL)
	if len(groups.Groups) != 1 || len(groups.Groups[0].Files) < 3 {
		t.Fatalf("expected one group with 3 files")
	}
	deletedPath := groups.Groups[0].Files[0].Path
	fileID := groups.Groups[0].Files[0].ID

	status, _ := deleteFile(t, baseURL, fileID)
	if status != http.StatusOK {
		t.Fatalf("delete file: status = %d, want 200", status)
	}
	if _, err := os.Stat(deletedPath); err == nil {
		t.Errorf("deleted file should not exist on disk")
	}
	assertGrouping(t, baseURL, 1, 2, sizeTriple)
}
