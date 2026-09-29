package client

import (
	"testing"
	"time"
)

// fakeEnv adapts a map to the env reader expected by WithEnv.
func fakeEnv(env map[string]string) func(string) string {
	return func(key string) string {
		return env[key]
	}
}

func TestNewConfigDefaults(t *testing.T) {
	cfg := NewConfig()
	if cfg.Addr != DefaultServerAddr {
		t.Fatalf("Addr = %q, want %q", cfg.Addr, DefaultServerAddr)
	}
	if cfg.TokenPath == "" || cfg.CachePath == "" {
		t.Fatalf("expected non-empty default paths: %+v", cfg)
	}
	if cfg.Timeout != DefaultCallTimeout {
		t.Fatalf("Timeout = %v, want %v", cfg.Timeout, DefaultCallTimeout)
	}
}

func TestWithOptions(t *testing.T) {
	cfg := NewConfig(
		WithAddress("example:1234"),
		WithTokenPath("/tmp/token.json"),
		WithCachePath("/tmp/cache.db"),
		WithTimeout(5*time.Second),
	)
	if cfg.Addr != "example:1234" {
		t.Fatalf("Addr = %q", cfg.Addr)
	}
	if cfg.TokenPath != "/tmp/token.json" {
		t.Fatalf("TokenPath = %q", cfg.TokenPath)
	}
	if cfg.CachePath != "/tmp/cache.db" {
		t.Fatalf("CachePath = %q", cfg.CachePath)
	}
	if cfg.Timeout != 5*time.Second {
		t.Fatalf("Timeout = %v", cfg.Timeout)
	}
}

func TestEmptyOptionsIgnored(t *testing.T) {
	cfg := NewConfig(WithAddress(""), WithTimeout(0))
	if cfg.Addr != DefaultServerAddr {
		t.Fatalf("Addr = %q", cfg.Addr)
	}
	if cfg.Timeout != DefaultCallTimeout {
		t.Fatalf("Timeout = %v", cfg.Timeout)
	}
}

func TestWithEnv(t *testing.T) {
	cfg := NewConfig(WithEnv(fakeEnv(map[string]string{
		"GOPHKEEPER_ADDR":    "env:9999",
		"GOPHKEEPER_TOKEN":   "/env/token.json",
		"GOPHKEEPER_CACHE":   "/env/cache.db",
		"GOPHKEEPER_TIMEOUT": "30s",
	})))
	if cfg.Addr != "env:9999" {
		t.Fatalf("Addr = %q", cfg.Addr)
	}
	if cfg.TokenPath != "/env/token.json" {
		t.Fatalf("TokenPath = %q", cfg.TokenPath)
	}
	if cfg.CachePath != "/env/cache.db" {
		t.Fatalf("CachePath = %q", cfg.CachePath)
	}
	if cfg.Timeout != 30*time.Second {
		t.Fatalf("Timeout = %v", cfg.Timeout)
	}
}

func TestWithEnvInvalidTimeoutIgnored(t *testing.T) {
	for _, v := range []string{"nope", "-5s", "0s"} {
		cfg := NewConfig(WithEnv(fakeEnv(map[string]string{"GOPHKEEPER_TIMEOUT": v})))
		if cfg.Timeout != DefaultCallTimeout {
			t.Fatalf("GOPHKEEPER_TIMEOUT=%q: Timeout = %v, want default", v, cfg.Timeout)
		}
	}
}

func TestWithEnvOverridesAfterOptions(t *testing.T) {
	cfg := NewConfig(WithAddress("explicit:1"), WithEnv(fakeEnv(nil)))
	if cfg.Addr != "explicit:1" {
		t.Fatalf("Addr = %q", cfg.Addr)
	}
}
