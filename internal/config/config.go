package config

import (
	"errors"
	"os"
	"strconv"
)

// Env names for configuration. Empty or unset means use default.
const (
	EnvDataDir = "DITTO_DATA_DIR"
	EnvPort    = "DITTO_PORT"
)

// Default values when env is unset.
const (
	DefaultDataDir = "./data"
	DefaultPort    = 8080
)

// Config holds application configuration loaded from the environment.
type Config struct {
	dataDir string
	port    int
}

// Load reads configuration from the environment. Defaults are used when
// DITTO_DATA_DIR or DITTO_PORT are unset or empty. Returns an error if
// DITTO_PORT is set but invalid (non-numeric or out of range 0-65535).
// Port 0 means "let the kernel choose an available port" (useful for tests).
func Load() (*Config, error) {
	dataDir := os.Getenv(EnvDataDir)
	if dataDir == "" {
		dataDir = DefaultDataDir
	}

	portStr := os.Getenv(EnvPort)
	if portStr == "" {
		return &Config{dataDir: dataDir, port: DefaultPort}, nil
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, errors.New("DITTO_PORT must be a number")
	}
	if port < 0 || port > 65535 {
		return nil, errors.New("DITTO_PORT must be between 0 and 65535")
	}

	return &Config{dataDir: dataDir, port: port}, nil
}

// DataDir returns the path to the data directory (for SQLite DB and other persistent data).
func (c *Config) DataDir() string {
	return c.dataDir
}

// Port returns the HTTP server port (for the Web UI in later phases).
func (c *Config) Port() int {
	return c.port
}
