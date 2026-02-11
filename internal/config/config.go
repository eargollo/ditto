package config

import (
	"errors"
	"os"
	"strconv"
)

// Env names for configuration. Empty or unset means use default (where applicable).
const (
	EnvDataDir     = "DITTO_DATA_DIR"
	EnvPort        = "DITTO_PORT"
	EnvDatabaseURL = "DATABASE_URL" // PostgreSQL connection URL (required for v0.2+)
)

// Default values when env is unset.
const (
	DefaultDataDir = "./data"
	DefaultPort    = 8080
)

// Config holds application configuration loaded from the environment.
type Config struct {
	dataDir     string
	port        int
	databaseURL string
}

// Load reads configuration from the environment. Defaults are used when
// DITTO_DATA_DIR or DITTO_PORT are unset or empty. Returns an error if
// DATABASE_URL is unset (required for v0.2+), or if DITTO_PORT is set but
// invalid (non-numeric or out of range 0-65535).
// Port 0 means "let the kernel choose an available port" (useful for tests).
func Load() (*Config, error) {
	dataDir := os.Getenv(EnvDataDir)
	if dataDir == "" {
		dataDir = DefaultDataDir
	}

	databaseURL := os.Getenv(EnvDatabaseURL)
	if databaseURL == "" {
		return nil, errors.New("DATABASE_URL is required (e.g. postgres://user:pass@localhost:5432/ditto?sslmode=disable)")
	}

	portStr := os.Getenv(EnvPort)
	if portStr == "" {
		return &Config{dataDir: dataDir, port: DefaultPort, databaseURL: databaseURL}, nil
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, errors.New("DITTO_PORT must be a number")
	}
	if port < 0 || port > 65535 {
		return nil, errors.New("DITTO_PORT must be between 0 and 65535")
	}

	return &Config{dataDir: dataDir, port: port, databaseURL: databaseURL}, nil
}

// DataDir returns the path to the data directory (for SQLite DB and other persistent data).
func (c *Config) DataDir() string {
	return c.dataDir
}

// Port returns the HTTP server port (for the Web UI in later phases).
func (c *Config) Port() int {
	return c.port
}

// DatabaseURL returns the PostgreSQL connection URL (required for v0.2+).
func (c *Config) DatabaseURL() string {
	return c.databaseURL
}
