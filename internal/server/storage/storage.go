// Package storage defines the persistence layer of the server.
package storage

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound is returned when the requested record does not exist.
var ErrNotFound = errors.New("not found")

// ErrAlreadyExists is returned when a unique constraint is violated.
var ErrAlreadyExists = errors.New("already exists")

// User is a registered account.
type User struct {
	ID            uuid.UUID
	Login         string
	PasswordHash  string
	KekSalt       []byte
	DekCiphertext []byte
	CreatedAt     time.Time
}

// Item is a single secret owned by a user.
type Item struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	Type       string
	Name       string
	Login      string
	Password   string
	Data       []byte
	CardNumber string
	CardExp    string
	CardCVV    string
	Meta       string
	UpdatedAt  time.Time
}

// File is a stored binary object; its content lives in the blob store.
type File struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Name      string
	Size      int64
	Meta      string
	S3Key     string
	UpdatedAt time.Time
}
