// Package storage defines the persistence layer of the server.
package storage

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// migrationsFS holds the SQL schema files applied at startup.
// The files are shared with sqlc (see sqlc.yaml).
//
//go:embed all:migrations/*.sql
var migrationsFS embed.FS

// Migrate applies the embedded schema migrations to the database.
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
		if _, err := pool.Exec(ctx, string(query)); err != nil && !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
	}
	return nil
}
