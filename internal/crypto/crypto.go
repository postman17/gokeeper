// Package crypto provides the KEK/DEK envelope encryption primitives used by
// the server to protect user secrets.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"golang.org/x/crypto/argon2"
)

// ErrEmptyBlob is returned when a ciphertext blob is too short to contain a nonce.
var ErrEmptyBlob = errors.New("crypto: empty ciphertext")

// DeriveKEK derives a 32-byte key-encryption key (KEK) from the master
// password and a per-user salt using Argon2id.
func DeriveKEK(masterPassword string, salt []byte) []byte {
	return argon2.IDKey([]byte(masterPassword), salt, 1, 64*1024, 4, 32)
}

// NewDEK returns a new random 32-byte data-encryption key (DEK).
func NewDEK() ([]byte, error) {
	dek := make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		return nil, fmt.Errorf("generate dek: %w", err)
	}
	return dek, nil
}

// NewSalt returns a new random 16-byte per-user salt.
func NewSalt() ([]byte, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}
	return salt, nil
}

// Encrypt seals plaintext with AES-256-GCM and returns nonce||ciphertext.
func Encrypt(key, plaintext []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt opens a nonce||ciphertext blob produced by Encrypt.
func Decrypt(key, blob []byte) ([]byte, error) {
	if len(blob) == 0 {
		return nil, ErrEmptyBlob
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(blob) < gcm.NonceSize() {
		return nil, errors.New("crypto: ciphertext too short")
	}
	nonce, ct := blob[:gcm.NonceSize()], blob[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return plaintext, nil
}

// EncryptString seals a text field and returns it as base64
// (StdEncoding) of nonce||ciphertext. An empty string stays empty.
func EncryptString(key []byte, s string) (string, error) {
	if s == "" {
		return "", nil
	}
	blob, err := Encrypt(key, []byte(s))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(blob), nil
}

// DecryptString reverses EncryptString. An empty string stays empty.
func DecryptString(key []byte, s string) (string, error) {
	if s == "" {
		return "", nil
	}
	blob, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return "", fmt.Errorf("decode base64: %w", err)
	}
	plaintext, err := Decrypt(key, blob)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// newGCM builds an AES-256-GCM cipher for the given 32-byte key.
func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("init aes: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("init gcm: %w", err)
	}
	return gcm, nil
}
