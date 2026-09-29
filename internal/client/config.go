// Package client provides the gRPC client used by the CLI.
package client

import (
	"os"
	"time"
)

// DefaultServerAddr is the address used when no other is configured.
const DefaultServerAddr = "localhost:3200"

// DefaultCallTimeout bounds each gRPC call unless overridden.
const DefaultCallTimeout = 60 * time.Second

// Config holds the client configuration.
type Config struct {
	// Addr is the gRPC server address.
	Addr string
	// TokenPath is the path to the stored token file.
	TokenPath string
	// CachePath is the path to the local SQLite cache.
	CachePath string
	// Timeout bounds each gRPC call so an unreachable server fails fast.
	Timeout time.Duration
}

// Option customizes a Config.
type Option func(*Config)

// WithAddress overrides the gRPC server address.
func WithAddress(addr string) Option {
	return func(c *Config) {
		if addr != "" {
			c.Addr = addr
		}
	}
}

// WithTokenPath overrides the token file location.
func WithTokenPath(path string) Option {
	return func(c *Config) {
		if path != "" {
			c.TokenPath = path
		}
	}
}

// WithCachePath overrides the cache file location.
func WithCachePath(path string) Option {
	return func(c *Config) {
		if path != "" {
			c.CachePath = path
		}
	}
}

// WithTimeout overrides the per-call timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Config) {
		if d > 0 {
			c.Timeout = d
		}
	}
}

// WithEnv applies overrides from the environment using the given reader
// (os.Getenv by default). Passing a nil reader is a no-op.
func WithEnv(getenv func(string) string) Option {
	return func(c *Config) {
		if getenv == nil {
			return
		}
		if v := getenv("GOPHKEEPER_ADDR"); v != "" {
			c.Addr = v
		}
		if v := getenv("GOPHKEEPER_TOKEN"); v != "" {
			c.TokenPath = v
		}
		if v := getenv("GOPHKEEPER_CACHE"); v != "" {
			c.CachePath = v
		}
		if v := getenv("GOPHKEEPER_TIMEOUT"); v != "" {
			if d, err := time.ParseDuration(v); err == nil && d > 0 {
				c.Timeout = d
			}
		}
	}
}

// NewConfig builds a Config from defaults and the given options.
func NewConfig(opts ...Option) Config {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	cfg := Config{
		Addr:      DefaultServerAddr,
		TokenPath: home + "/.gophkeeper/token.json",
		CachePath: home + "/.gophkeeper/cache.db",
		Timeout:   DefaultCallTimeout,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}
