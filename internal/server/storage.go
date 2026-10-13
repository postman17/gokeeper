package server

import (
	"context"

	"github.com/google/uuid"

	"github.com/kmorozov/gophkeeper/internal/server/storage"
)

// ErrNotFound is re-exported from the storage implementation for callers of
// this package.
var ErrNotFound = storage.ErrNotFound

// ErrAlreadyExists is re-exported from the storage implementation for callers
// of this package.
var ErrAlreadyExists = storage.ErrAlreadyExists

// Storage is the persistence interface consumed by the GophKeeper service.
// It lives next to its consumer; the concrete implementation is provided by
// the storage package.
type Storage interface {
	// CreateUser registers a new user and returns the generated id.
	CreateUser(ctx context.Context, login, passwordHash string, kekSalt, dekCiphertext []byte) (uuid.UUID, error)
	// GetUserByLogin returns the user with the given login.
	GetUserByLogin(ctx context.Context, login string) (storage.User, error)
	// GetUserByID returns the user with the given id.
	GetUserByID(ctx context.Context, id uuid.UUID) (storage.User, error)
	// CreateItem stores a new item and returns the generated id.
	CreateItem(ctx context.Context, item storage.Item) (uuid.UUID, error)
	// GetItem returns the item with the given id if it belongs to the user.
	GetItem(ctx context.Context, userID, id uuid.UUID) (storage.Item, error)
	// ListItems returns all items of the user.
	ListItems(ctx context.Context, userID uuid.UUID) ([]storage.Item, error)
	// DeleteItem removes the item with the given id if it belongs to the user.
	DeleteItem(ctx context.Context, userID, id uuid.UUID) (bool, error)
	// CreateFile stores a file record; if file.ID is set it is used, otherwise
	// a new id is generated and returned.
	CreateFile(ctx context.Context, file storage.File) (uuid.UUID, error)
	// GetFile returns the file record with the given id if it belongs to the user.
	GetFile(ctx context.Context, userID, id uuid.UUID) (storage.File, error)
	// ListFiles returns all file records of the user.
	ListFiles(ctx context.Context, userID uuid.UUID) ([]storage.File, error)
	// DeleteFile removes the file record with the given id if it belongs to
	// the user and returns it so the stored blob can be removed as well.
	DeleteFile(ctx context.Context, userID, id uuid.UUID) (storage.File, bool, error)
	// Ping checks connectivity to the database.
	Ping(ctx context.Context) error
	// Close releases all resources held by the storage.
	Close() error
}
