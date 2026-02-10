package server

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/eargollo/ditto/internal/config"
	"github.com/eargollo/ditto/internal/db"
)

func testServer(t *testing.T) (*Server, *sql.DB) {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	cfg := &config.Config{}
	// Use config's default port for tests (we don't need to listen)
	srv, err := NewServer(cfg, database, nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv, database
}

func TestServer_HomeReturns200AndContainsDitto(t *testing.T) {
	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /: code = %d, want 200", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), "Ditto") {
		t.Errorf("GET /: body does not contain Ditto: %s", body)
	}
}

func TestServer_HealthReturns200(t *testing.T) {
	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /health: code = %d, want 200", rec.Code)
	}
}

func TestServer_ScansReturns200WithScansContent(t *testing.T) {
	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/scans", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /scans: code = %d, want 200", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), "Recent scans") {
		t.Errorf("GET /scans: body should contain 'Recent scans': %s", body)
	}
}

func TestServer_FragmentReturnsHTML(t *testing.T) {
	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/fragment", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /api/fragment: code = %d, want 200", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), "HTMX") {
		t.Errorf("GET /api/fragment: body should contain HTMX: %s", body)
	}
}

func TestServer_404ForUnknown(t *testing.T) {
	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/unknown", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /unknown: code = %d, want 404", rec.Code)
	}
}

func TestServer_RunContextCancelShutsDown(t *testing.T) {
	// Port 0 so the server binds to an available port (avoids "address already in use" when 8080 is taken)
	os.Setenv(config.EnvPort, "0")
	defer os.Unsetenv(config.EnvPort)
	cfg, _ := config.Load()
	database, _ := db.Open(":memory:")
	defer database.Close()
	_ = db.Migrate(database)
	srv, err := NewServer(cfg, database, nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so Run returns quickly
	err = srv.Run(ctx)
	if err != nil && err != http.ErrServerClosed {
		t.Errorf("Run after cancel: err = %v", err)
	}
}
