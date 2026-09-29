package crypto

import (
	"bytes"
	"testing"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	key, err := NewDEK()
	if err != nil {
		t.Fatalf("NewDEK: %v", err)
	}
	return key
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	key := testKey(t)
	plaintext := []byte("super secret payload")
	blob, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	got, err := Decrypt(key, blob)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("roundtrip mismatch: got %q, want %q", got, plaintext)
	}
}

func TestDecryptWrongKeyFails(t *testing.T) {
	blob, err := Encrypt(testKey(t), []byte("data"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := Decrypt(testKey(t), blob); err == nil {
		t.Fatal("expected error with wrong key, got nil")
	}
}

func TestEncryptDecryptStringRoundtrip(t *testing.T) {
	key := testKey(t)
	for _, s := range []string{"hello", "", "пароль 123"} {
		enc, err := EncryptString(key, s)
		if err != nil {
			t.Fatalf("EncryptString(%q): %v", s, err)
		}
		got, err := DecryptString(key, enc)
		if err != nil {
			t.Fatalf("DecryptString(%q): %v", enc, err)
		}
		if got != s {
			t.Fatalf("roundtrip mismatch: got %q, want %q", got, s)
		}
	}
}

func TestEmptyStringStaysEmpty(t *testing.T) {
	key := testKey(t)
	enc, err := EncryptString(key, "")
	if err != nil {
		t.Fatalf("EncryptString: %v", err)
	}
	if enc != "" {
		t.Fatalf("expected empty ciphertext for empty string, got %q", enc)
	}
	dec, err := DecryptString(key, "")
	if err != nil {
		t.Fatalf("DecryptString: %v", err)
	}
	if dec != "" {
		t.Fatalf("expected empty plaintext, got %q", dec)
	}
}

func TestTamperedBlobFails(t *testing.T) {
	key := testKey(t)
	blob, err := Encrypt(key, []byte("data"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	blob[len(blob)-1] ^= 0xff
	if _, err := Decrypt(key, blob); err == nil {
		t.Fatal("expected error for tampered blob, got nil")
	}
}

func TestDecryptEmptyBlobFails(t *testing.T) {
	if _, err := Decrypt(testKey(t), nil); err == nil {
		t.Fatal("expected error for empty blob, got nil")
	}
}

func TestDeriveKEK(t *testing.T) {
	salt, err := NewSalt()
	if err != nil {
		t.Fatalf("NewSalt: %v", err)
	}
	if len(salt) != 16 {
		t.Fatalf("salt length = %d, want 16", len(salt))
	}
	kek := DeriveKEK("master", salt)
	if len(kek) != 32 {
		t.Fatalf("kek length = %d, want 32", len(kek))
	}
	if other := DeriveKEK("other", salt); bytes.Equal(kek, other) {
		t.Fatal("expected different KEKs for different master passwords")
	}
	if other := DeriveKEK("master", []byte("anothersalt12345")); bytes.Equal(kek, other) {
		t.Fatal("expected different KEKs for different salts")
	}
}
