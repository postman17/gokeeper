package client

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// ErrNoToken is returned when there is no stored token yet.
var ErrNoToken = errors.New("no stored token, run login first")

// TokenRecord is the JSON representation of the stored token file.
type TokenRecord struct {
	Token     string    `json:"token"`
	UserID    string    `json:"user_id"`
	Login     string    `json:"login"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TokenStore persists the auth token on disk with restrictive permissions.
type TokenStore struct {
	path string
}

// NewTokenStore creates a TokenStore rooted at the given file path.
func NewTokenStore(path string) *TokenStore {
	return &TokenStore{path: path}
}

// Save writes the token record to disk with 0600 permissions.
func (s *TokenStore) Save(rec TokenRecord) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

// Load reads the stored token record.
func (s *TokenStore) Load() (TokenRecord, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return TokenRecord{}, ErrNoToken
	}
	if err != nil {
		return TokenRecord{}, err
	}
	var rec TokenRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return TokenRecord{}, err
	}
	if rec.Token == "" {
		return TokenRecord{}, ErrNoToken
	}
	return rec, nil
}

// Clear removes the stored token file.
func (s *TokenStore) Clear() error {
	err := os.Remove(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
