// Package storage defines the persistence layer of the server.
package storage

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// migrationsFS holds the SQL schema files applied at startup.
// The files are shared with sqlc (see sqlc.yaml).
//
//go:embed all:migrations/*.sql
var migrationsFS embed.FS

// Migrate applies the embedded schema migrations to the database.
// Already-applied objects (duplicate tables or schema objects) are skipped;
// any other error is returned.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	entries, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	for _, name := range entries {
		query, err := migrationsFS.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		if _, err := pool.Exec(ctx, string(query)); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && (pgErr.Code == pgerrcode.DuplicateTable || pgErr.Code == pgerrcode.DuplicateObject) {
				continue
			}
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
	}
	return nil
}
