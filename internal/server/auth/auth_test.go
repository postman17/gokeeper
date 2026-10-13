package auth

import (
	"strings"
	"testing"
	"time"
)

// TestHashPasswordRoundtrip checks that a password survives the hash/verify cycle.
func TestHashPasswordRoundtrip(t *testing.T) {
	hash, err := HashPassword("s3cr3t-pass")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "s3cr3t-pass" || len(hash) == 0 {
		t.Fatalf("expected bcrypt hash, got %q", hash)
	}
	if !CheckPassword("s3cr3t-pass", hash) {
		t.Fatal("CheckPassword must accept the correct password")
	}
	if CheckPassword("wrong", hash) {
		t.Fatal("CheckPassword must reject a wrong password")
	}
}

// TestNewTokenDeterministicSalt checks that hashes differ between calls.
func TestHashPasswordDifferentSalts(t *testing.T) {
	h1, _ := HashPassword("same")
	h2, _ := HashPassword("same")
	if h1 == h2 {
		t.Fatal("bcrypt hashes of the same password must differ due to salt")
	}
}

// TestTokenRoundtrip checks that a token parses back to the same user ID.
func TestTokenRoundtrip(t *testing.T) {
	m := NewJWTManager("test-secret", time.Hour)
	token, err := m.NewToken("11111111-1111-1111-1111-111111111111")
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}
	if strings.Count(token, ".") != 2 {
		t.Fatalf("expected a compact JWS token, got %q", token)
	}
	uid, err := m.ParseToken(token)
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}
	if uid != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("got user id %q", uid)
	}
}

// TestParseTokenExpired checks that expired tokens are rejected.
func TestParseTokenExpired(t *testing.T) {
	m := NewJWTManager("test-secret", -time.Minute)
	token, err := m.NewToken("user")
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}
	if _, err := m.ParseToken(token); err == nil {
		t.Fatal("expired token must not parse")
	}
}

// TestParseTokenWrongSecret checks that tokens signed with another secret are rejected.
func TestParseTokenWrongSecret(t *testing.T) {
	issuer := NewJWTManager("secret-a", time.Hour)
	verifier := NewJWTManager("secret-b", time.Hour)
	token, err := issuer.NewToken("user")
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}
	if _, err := verifier.ParseToken(token); err == nil {
		t.Fatal("token signed with a different secret must not parse")
	}
}

// TestParseTokenGarbage checks that malformed tokens are rejected.
func TestParseTokenGarbage(t *testing.T) {
	m := NewJWTManager("test-secret", time.Hour)
	if _, err := m.ParseToken("not.a.jwt"); err == nil {
		t.Fatal("garbage token must not parse")
	}
}
