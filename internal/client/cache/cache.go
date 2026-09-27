// Package cache provides a local SQLite-backed cache for previously fetched secrets.
package cache

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	pb "github.com/kmorozov/gophkeeper/proto/gophkeeper/v1"

	_ "modernc.org/sqlite"
)

// Item is the cached representation of a secret.
type Item struct {
	ID         string
	Type       pb.ItemType
	Name       string
	Login      string
	Password   string
	Data       []byte
	CardNumber string
	CardExp    string
	CardCvv    string
	Meta       string
	UpdatedAt  time.Time
}

// Cache stores secrets in a local SQLite database.
type Cache struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS items (
	id TEXT PRIMARY KEY,
	type TEXT NOT NULL,
	name TEXT NOT NULL DEFAULT '',
	login TEXT NOT NULL DEFAULT '',
	password TEXT NOT NULL DEFAULT '',
	data BLOB NOT NULL DEFAULT x'',
	card_number TEXT NOT NULL DEFAULT '',
	card_exp TEXT NOT NULL DEFAULT '',
	card_cvv TEXT NOT NULL DEFAULT '',
	meta TEXT NOT NULL DEFAULT '',
	updated_at TIMESTAMP NOT NULL,
	cached_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
)`

// New opens (or creates) the cache database at path.
func New(path string) (*Cache, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open cache: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init cache: %w", err)
	}
	return &Cache{db: db}, nil
}

// Close releases the cache database.
func (c *Cache) Close() error {
	return c.db.Close()
}

// PutItem upserts a single item into the cache.
func (c *Cache) PutItem(ctx context.Context, item Item) error {
	return c.PutItems(ctx, []Item{item})
}

// PutItems upserts items into the cache in a single transaction.
func (c *Cache) PutItems(ctx context.Context, items []Item) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, it := range items {
		if _, err := tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO items
				(id, type, name, login, password, data, card_number, card_exp, card_cvv, meta, updated_at, cached_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
			it.ID, it.Type.String(), it.Name, it.Login, it.Password, it.Data,
			it.CardNumber, it.CardExp, it.CardCvv, it.Meta, it.UpdatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetItem returns the cached item by id and whether it was found.
func (c *Cache) GetItem(ctx context.Context, id string) (Item, bool, error) {
	row := c.db.QueryRowContext(ctx, `
		SELECT id, type, name, login, password, data, card_number, card_exp, card_cvv, meta, updated_at
		FROM items WHERE id = ?`, id)
	item, err := scanItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, false, nil
	}
	if err != nil {
		return Item{}, false, err
	}
	return item, true, nil
}

// ListItems returns all cached items ordered by updated_at descending.
func (c *Cache) ListItems(ctx context.Context) ([]Item, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT id, type, name, login, password, data, card_number, card_exp, card_cvv, meta, updated_at
		FROM items ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Item
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// Delete removes an item from the cache.
func (c *Cache) Delete(ctx context.Context, id string) error {
	_, err := c.db.ExecContext(ctx, `DELETE FROM items WHERE id = ?`, id)
	return err
}

// scanner abstracts *sql.Row and *sql.Rows for item scanning.
type scanner interface {
	Scan(dest ...any) error
}

// scanItem reads a single row into an Item.
func scanItem(sc scanner) (Item, error) {
	var item Item
	var typeStr string
	if err := sc.Scan(&item.ID, &typeStr, &item.Name, &item.Login, &item.Password, &item.Data,
		&item.CardNumber, &item.CardExp, &item.CardCvv, &item.Meta, &item.UpdatedAt); err != nil {
		return Item{}, err
	}
	item.Type = pb.ItemType(pb.ItemType_value[typeStr])
	return item, nil
}
