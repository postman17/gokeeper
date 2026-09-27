// Package client provides the gRPC client used by the CLI.
package client

import (
	"os"
)

// DefaultServerAddr is the address used when no other is configured.
const DefaultServerAddr = "localhost:3200"

// Config holds the client configuration.
type Config struct {
	// Addr is the gRPC server address.
	Addr string
	// TokenPath is the path to the stored token file.
	TokenPath string
	// CachePath is the path to the local SQLite cache.
	CachePath string
}

// NewConfig builds a Config from the environment with defaults.
func NewConfig() Config {
	addr := os.Getenv("GOPHKEEPER_ADDR")
	if addr == "" {
		addr = DefaultServerAddr
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	tokenPath := os.Getenv("GOPHKEEPER_TOKEN")
	if tokenPath == "" {
		tokenPath = home + "/.gophkeeper/token.json"
	}
	cachePath := os.Getenv("GOPHKEEPER_CACHE")
	if cachePath == "" {
		cachePath = home + "/.gophkeeper/cache.db"
	}
	return Config{Addr: addr, TokenPath: tokenPath, CachePath: cachePath}
}
