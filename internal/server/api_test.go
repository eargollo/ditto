package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/eargollo/ditto/internal/db"
)

// API tests (TDD): define expected behaviour of /api/* routes; implement handlers to make these pass.

func TestAPI_Health_returns200(t *testing.T) {
	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /api/health: code = %d, want 200", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if string(body) != "ok" {
		t.Errorf("GET /api/health: body = %q, want \"ok\"", body)
	}
}

func TestAPI_Roots_listEmptyReturns200AndArray(t *testing.T) {
	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/roots", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /api/roots: code = %d, want 200", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("GET /api/roots: Content-Type = %q, want application/json", rec.Header().Get("Content-Type"))
	}
	body, _ := io.ReadAll(rec.Body)
	var roots []map[string]interface{}
	if err := json.Unmarshal(body, &roots); err != nil {
		t.Fatalf("GET /api/roots: invalid JSON: %v", err)
	}
	if len(roots) != 0 {
		t.Errorf("GET /api/roots: len(roots) = %d, want 0", len(roots))
	}
}

func TestAPI_Roots_postThenListReturnsRoot(t *testing.T) {
	srv, _ := testServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/roots", bytes.NewReader([]byte(`{"path":"/tmp/test-root"}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Errorf("POST /api/roots: code = %d, want 201", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if loc == "" {
		t.Error("POST /api/roots: want Location header")
	}
	var created map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("POST /api/roots: invalid JSON body: %v", err)
	}
	if created["path"] != "/tmp/test-root" {
		t.Errorf("POST /api/roots: body path = %v, want /tmp/test-root", created["path"])
	}
	idNum, ok := created["id"].(float64)
	if !ok || idNum < 1 {
		t.Errorf("POST /api/roots: body id = %v, want positive number", created["id"])
	}

	// GET /api/roots should now return one root
	req2 := httptest.NewRequest(http.MethodGet, "/api/roots", nil)
	rec2 := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("GET /api/roots after POST: code = %d, want 200", rec2.Code)
	}
	var roots []map[string]interface{}
	if err := json.NewDecoder(rec2.Body).Decode(&roots); err != nil {
		t.Fatalf("GET /api/roots: %v", err)
	}
	if len(roots) != 1 {
		t.Fatalf("GET /api/roots: len = %d, want 1", len(roots))
	}
	if roots[0]["path"] != "/tmp/test-root" {
		t.Errorf("GET /api/roots: path = %v, want /tmp/test-root", roots[0]["path"])
	}
	_ = idNum
}

func TestAPI_Roots_getByID_returns200WithRoot(t *testing.T) {
	srv, database := testServer(t)
	ctx := context.Background()
	id, err := db.AddScanRoot(ctx, database, "/data/foo")
	if err != nil {
		t.Fatalf("setup: AddScanRoot: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/roots/"+strconv.FormatInt(id, 10), nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /api/roots/%d: code = %d, want 200", id, rec.Code)
	}
	var root map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&root); err != nil {
		t.Fatalf("GET /api/roots/%d: %v", id, err)
	}
	if root["path"] != "/data/foo" {
		t.Errorf("GET /api/roots/%d: path = %v, want /data/foo", id, root["path"])
	}
}

func TestAPI_Roots_getByID_returns404WhenMissing(t *testing.T) {
	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/roots/99999", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /api/roots/99999: code = %d, want 404", rec.Code)
	}
}

func TestAPI_Scans_listEmptyReturns200AndArray(t *testing.T) {
	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/scans", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /api/scans: code = %d, want 200", rec.Code)
	}
	var scans []map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&scans); err != nil {
		t.Fatalf("GET /api/scans: %v", err)
	}
	if len(scans) != 0 {
		t.Errorf("GET /api/scans: len = %d, want 0", len(scans))
	}
}

func TestAPI_Scans_postWithRootPath_returns202AndLocation(t *testing.T) {
	srv, database := testServer(t)
	ctx := context.Background()
	folderID, err := db.GetOrCreateFolderByPath(ctx, database, "/tmp/scan-me")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	_ = folderID

	req := httptest.NewRequest(http.MethodPost, "/api/scans", bytes.NewReader([]byte(`{"root_path":"/tmp/scan-me"}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Errorf("POST /api/scans: code = %d, want 202", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if loc == "" {
		t.Error("POST /api/scans: want Location header")
	}
	var scan map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&scan); err != nil {
		t.Fatalf("POST /api/scans: %v", err)
	}
	if scan["root_path"] != "/tmp/scan-me" {
		t.Errorf("POST /api/scans: root_path = %v", scan["root_path"])
	}
}

func TestAPI_Scans_getByID_returns404WhenMissing(t *testing.T) {
	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/scans/99999", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /api/scans/99999: code = %d, want 404", rec.Code)
	}
	var errBody map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&errBody); err == nil {
		if errBody["error"] == nil {
			t.Error("GET /api/scans/99999: want error field in JSON body")
		}
	}
}

func TestAPI_Scans_status_returns404WhenMissing(t *testing.T) {
	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/scans/99999/status", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /api/scans/99999/status: code = %d, want 404", rec.Code)
	}
}

func TestAPI_Duplicates_summary_returns200AndObject(t *testing.T) {
	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/duplicates/summary", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /api/duplicates/summary: code = %d, want 200", rec.Code)
	}
	var summary map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&summary); err != nil {
		t.Fatalf("GET /api/duplicates/summary: %v", err)
	}
	for _, key := range []string{"group_count", "total_files", "total_size", "reclaimable_size"} {
		if _, ok := summary[key]; !ok {
			t.Errorf("GET /api/duplicates/summary: missing key %q", key)
		}
	}
}

func TestAPI_Duplicates_groups_returns200AndGroupsTotal(t *testing.T) {
	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/duplicates/groups?limit=5&offset=0", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /api/duplicates/groups: code = %d, want 200", rec.Code)
	}
	var out map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("GET /api/duplicates/groups: %v", err)
	}
	if _, ok := out["groups"]; !ok {
		t.Error("GET /api/duplicates/groups: missing groups")
	}
	if _, ok := out["total"]; !ok {
		t.Error("GET /api/duplicates/groups: missing total")
	}
}

func TestAPI_PostScans_invalidJSON_returns400WithError(t *testing.T) {
	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/scans", bytes.NewReader([]byte(`{invalid`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("POST /api/scans invalid JSON: code = %d, want 400", rec.Code)
	}
	var errBody map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&errBody); err != nil {
		t.Fatalf("response JSON: %v", err)
	}
	if errBody["error"] == nil {
		t.Error("want error field in 400 response")
	}
}

// --- POST /api/duplicates/files/delete ---

func TestAPI_DeleteFile_getReturns404(t *testing.T) {
	srv, _ := testServer(t)
	// Route is registered for POST only; GET does not match and falls through to 404.
	req := httptest.NewRequest(http.MethodGet, "/api/duplicates/files/delete", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /api/duplicates/files/delete: code = %d, want 404", rec.Code)
	}
}

func TestAPI_DeleteFile_invalidJSON_returns400(t *testing.T) {
	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/duplicates/files/delete", bytes.NewReader([]byte(`{invalid`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("POST delete invalid JSON: code = %d, want 400", rec.Code)
	}
}

func TestAPI_DeleteFile_missingOrInvalidFileID_returns400(t *testing.T) {
	srv, _ := testServer(t)
	for _, body := range []string{`{}`, `{"file_id":0}`, `{"file_id":-1}`} {
		req := httptest.NewRequest(http.MethodPost, "/api/duplicates/files/delete", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("POST delete %s: code = %d, want 400", body, rec.Code)
		}
	}
}

func TestAPI_DeleteFile_notFound_returns400(t *testing.T) {
	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/duplicates/files/delete", bytes.NewReader([]byte(`{"file_id":99999}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("POST delete file_id=99999: code = %d, want 400", rec.Code)
	}
	var errBody map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&errBody); err != nil {
		t.Fatalf("response JSON: %v", err)
	}
	if errBody["error"] == nil {
		t.Error("want error field in 400 response")
	}
}

func TestAPI_DeleteFile_fileHasNoHash_returns400(t *testing.T) {
	srv, database := testServer(t)
	ctx := context.Background()
	folderID, err := db.AddFolder(ctx, database, "/tmp/unit-delete")
	if err != nil {
		t.Fatalf("AddFolder: %v", err)
	}
	fileID, err := db.UpsertFile(ctx, database, folderID, "no-hash.txt", 100, 12345, nil, nil)
	if err != nil {
		t.Fatalf("UpsertFile: %v", err)
	}
	// File exists but has hash_status=pending and no hash; delete should reject
	req := httptest.NewRequest(http.MethodPost, "/api/duplicates/files/delete", bytes.NewReader([]byte(`{"file_id":`+strconv.FormatInt(fileID, 10)+`}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("POST delete file with no hash: code = %d, want 400", rec.Code)
	}
	var errBody map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&errBody); err != nil {
		t.Fatalf("response JSON: %v", err)
	}
	if errBody["error"] == nil {
		t.Error("want error field in 400 response")
	}
}
