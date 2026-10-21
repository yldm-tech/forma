package database

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"

	// Registers the "pgx" database/sql driver goose runs on.
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/yldm-tech/forma/server/migrations"
)

// NewMigrator opens a dedicated connection for schema changes, separate from the request pool. The returned cleanup closes it.
func NewMigrator(databaseURL string, migrationsFS fs.FS) (*goose.Provider, func(), error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("open migration connection: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrationsFS)
	if err != nil {
		_ = db.Close()
		return nil, nil, fmt.Errorf("load migrations: %w", err)
	}
	return provider, func() { _ = db.Close() }, nil
}

// MigrateUp applies every pending migration in order, each in its own transaction, and logs what it applied.
func MigrateUp(ctx context.Context, databaseURL string, log *slog.Logger) error {
	provider, cleanup, err := NewMigrator(databaseURL, migrations.FS)
	if err != nil {
		return err
	}
	defer cleanup()

	results, err := provider.Up(ctx)
	for _, r := range results {
		log.InfoContext(ctx, "migration applied", "version", r.Source.Version, "file", r.Source.Path, "duration", r.Duration)
	}
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	if len(results) == 0 {
		log.InfoContext(ctx, "database schema is up to date")
	}
	return nil
}
