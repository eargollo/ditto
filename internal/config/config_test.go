package config

import (
	"testing"
)

const testDatabaseURL = "postgres://localhost/ditto?sslmode=disable"

func TestLoad_usesDefaultsWhenEnvUnset(t *testing.T) {
	t.Setenv("DITTO_DATA_DIR", "")
	t.Setenv("DITTO_PORT", "")
	t.Setenv("DATABASE_URL", testDatabaseURL)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() err = %v, want nil", err)
	}
	if cfg.DataDir() != "./data" {
		t.Errorf("DataDir() = %q, want %q", cfg.DataDir(), "./data")
	}
	if cfg.Port() != 8080 {
		t.Errorf("Port() = %d, want 8080", cfg.Port())
	}
	if cfg.DatabaseURL() != testDatabaseURL {
		t.Errorf("DatabaseURL() = %q, want %q", cfg.DatabaseURL(), testDatabaseURL)
	}
}

func TestLoad_usesEnvWhenSet(t *testing.T) {
	t.Setenv("DITTO_DATA_DIR", "/tmp/ditto")
	t.Setenv("DITTO_PORT", "9090")
	t.Setenv("DATABASE_URL", testDatabaseURL)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() err = %v, want nil", err)
	}
	if cfg.DataDir() != "/tmp/ditto" {
		t.Errorf("DataDir() = %q, want %q", cfg.DataDir(), "/tmp/ditto")
	}
	if cfg.Port() != 9090 {
		t.Errorf("Port() = %d, want 9090", cfg.Port())
	}
}

func TestLoad_returnsErrorWhenDatabaseURLUnset(t *testing.T) {
	t.Setenv("DITTO_DATA_DIR", "")
	t.Setenv("DITTO_PORT", "")
	t.Setenv("DATABASE_URL", "")

	_, err := Load()
	if err == nil {
		t.Error("Load() err = nil, want non-nil when DATABASE_URL unset")
	}
}

func TestLoad_returnsErrorForInvalidPort(t *testing.T) {
	t.Setenv("DITTO_DATA_DIR", "")
	t.Setenv("DITTO_PORT", "not-a-number")
	t.Setenv("DATABASE_URL", testDatabaseURL)

	_, err := Load()
	if err == nil {
		t.Error("Load() err = nil, want non-nil for invalid port")
	}
}

func TestLoad_returnsErrorForNegativePort(t *testing.T) {
	t.Setenv("DITTO_DATA_DIR", "")
	t.Setenv("DITTO_PORT", "-1")
	t.Setenv("DATABASE_URL", testDatabaseURL)

	_, err := Load()
	if err == nil {
		t.Error("Load() err = nil, want non-nil for negative port")
	}
}
