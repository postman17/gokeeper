package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kmorozov/gophkeeper/internal/server/storage/db"
)

// PostgresStorage is a Storage implementation backed by PostgreSQL
// using sqlc-generated queries.
type PostgresStorage struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

// NewPostgresStorage creates a connection pool, applies migrations and
// returns a storage ready to use.
func NewPostgresStorage(ctx context.Context, dsn string) (*PostgresStorage, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	if err := Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return &PostgresStorage{pool: pool, queries: db.New(pool)}, nil
}

// mapError converts low-level driver errors into storage sentinel errors.
func mapError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
		return ErrAlreadyExists
	}
	return err
}

// CreateUser registers a new user and returns the generated id.
func (s *PostgresStorage) CreateUser(ctx context.Context, login, passwordHash string, kekSalt, dekCiphertext []byte) (uuid.UUID, error) {
	u, err := s.queries.CreateUser(ctx, db.CreateUserParams{
		ID:            uuid.New(),
		Login:         login,
		PasswordHash:  passwordHash,
		KekSalt:       kekSalt,
		DekCiphertext: dekCiphertext,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("insert user: %w", mapError(err))
	}
	return u.ID, nil
}

// GetUserByLogin returns the user with the given login.
func (s *PostgresStorage) GetUserByLogin(ctx context.Context, login string) (User, error) {
	u, err := s.queries.GetUserByLogin(ctx, login)
	if err != nil {
		return User{}, fmt.Errorf("select user: %w", mapError(err))
	}
	return newUser(u), nil
}

// GetUserByID returns the user with the given id.
func (s *PostgresStorage) GetUserByID(ctx context.Context, id uuid.UUID) (User, error) {
	u, err := s.queries.GetUserByID(ctx, id)
	if err != nil {
		return User{}, fmt.Errorf("select user: %w", mapError(err))
	}
	return newUser(u), nil
}

// newUser converts a sqlc-generated row into the storage User type.
func newUser(u db.User) User {
	return User{
		ID:            u.ID,
		Login:         u.Login,
		PasswordHash:  u.PasswordHash,
		KekSalt:       u.KekSalt,
		DekCiphertext: u.DekCiphertext,
		CreatedAt:     u.CreatedAt,
	}
}

// CreateItem stores a new item and returns the generated id.
func (s *PostgresStorage) CreateItem(ctx context.Context, item Item) (uuid.UUID, error) {
	params := db.CreateItemParams{
		ID:         uuid.New(),
		UserID:     item.UserID,
		Type:       item.Type,
		Name:       item.Name,
		Login:      item.Login,
		Password:   item.Password,
		Data:       item.Data,
		CardNumber: item.CardNumber,
		CardExp:    item.CardExp,
		CardCvv:    item.CardCVV,
		Meta:       item.Meta,
		UpdatedAt:  time.Now(),
	}
	it, err := s.queries.CreateItem(ctx, params)
	if err != nil {
		return uuid.Nil, fmt.Errorf("insert item: %w", err)
	}
	return it.ID, nil
}

// newItem converts a sqlc-generated row into the storage Item type.
func newItem(row db.Item) Item {
	return Item{
		ID:         row.ID,
		UserID:     row.UserID,
		Type:       row.Type,
		Name:       row.Name,
		Login:      row.Login,
		Password:   row.Password,
		Data:       row.Data,
		CardNumber: row.CardNumber,
		CardExp:    row.CardExp,
		CardCVV:    row.CardCvv,
		Meta:       row.Meta,
		UpdatedAt:  row.UpdatedAt,
	}
}

// GetItem returns the item with the given id if it belongs to the user.
func (s *PostgresStorage) GetItem(ctx context.Context, userID, id uuid.UUID) (Item, error) {
	row, err := s.queries.GetItem(ctx, db.GetItemParams{ID: id, UserID: userID})
	if err != nil {
		return Item{}, fmt.Errorf("select item: %w", mapError(err))
	}
	return newItem(row), nil
}

// ListItems returns all items of the user.
func (s *PostgresStorage) ListItems(ctx context.Context, userID uuid.UUID) ([]Item, error) {
	rows, err := s.queries.ListItems(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("select items: %w", err)
	}
	items := make([]Item, 0, len(rows))
	for _, row := range rows {
		items = append(items, newItem(row))
	}
	return items, nil
}

// DeleteItem removes the item with the given id if it belongs to the user.
func (s *PostgresStorage) DeleteItem(ctx context.Context, userID, id uuid.UUID) (bool, error) {
	n, err := s.queries.DeleteItem(ctx, db.DeleteItemParams{ID: id, UserID: userID})
	if err != nil {
		return false, fmt.Errorf("delete item: %w", err)
	}
	return n > 0, nil
}

// Ping checks connectivity to the database.
func (s *PostgresStorage) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// Close releases the connection pool.
func (s *PostgresStorage) Close() error {
	s.pool.Close()
	return nil
}
