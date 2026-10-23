package database

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/jackc/pgx/v5"
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

// migrationLockKey is the session advisory lock every `forma migrate` and `forma start` holds while it migrates, so replicas that start at once apply each migration exactly once.
const migrationLockKey int64 = 0x666f726d612d6d67 // "forma-mg"

// WithMigrationLock runs fn while holding a PostgreSQL session advisory lock on a dedicated connection, waiting for any other migrator to finish first. goose and River each apply their migrations in transactions but take no lock across processes, so two pods migrating together would race on the same versions.
func WithMigrationLock(ctx context.Context, databaseURL string, log *slog.Logger, fn func(context.Context) error) error {
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("open migration lock connection: %w", err)
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()

	var acquired bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", migrationLockKey).Scan(&acquired); err != nil {
		return fmt.Errorf("take migration lock: %w", err)
	}
	if !acquired {
		log.InfoContext(ctx, "waiting for another process to finish migrating")
		if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockKey); err != nil {
			return fmt.Errorf("take migration lock: %w", err)
		}
	}
	// Closing the connection releases the lock too; unlocking first keeps it from outliving fn when Close is slow.
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", migrationLockKey)
	}()

	return fn(ctx)
}
