// Package storage defines the persistence layer of the server.
package storage

import (
	"context"
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

// Storage is the persistence interface used by the server.
type Storage interface {
	// CreateUser registers a new user and returns the generated id.
	CreateUser(ctx context.Context, login, passwordHash string, kekSalt, dekCiphertext []byte) (uuid.UUID, error)
	// GetUserByLogin returns the user with the given login.
	GetUserByLogin(ctx context.Context, login string) (User, error)
	// GetUserByID returns the user with the given id.
	GetUserByID(ctx context.Context, id uuid.UUID) (User, error)
	// CreateItem stores a new item and returns the generated id.
	CreateItem(ctx context.Context, item Item) (uuid.UUID, error)
	// GetItem returns the item with the given id if it belongs to the user.
	GetItem(ctx context.Context, userID, id uuid.UUID) (Item, error)
	// ListItems returns all items of the user.
	ListItems(ctx context.Context, userID uuid.UUID) ([]Item, error)
	// DeleteItem removes the item with the given id if it belongs to the user.
	DeleteItem(ctx context.Context, userID, id uuid.UUID) (bool, error)
	// Ping checks connectivity to the database.
	Ping(ctx context.Context) error
	// Close releases all resources held by the storage.
	Close() error
}
