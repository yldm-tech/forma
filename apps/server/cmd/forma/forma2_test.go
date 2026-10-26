package main

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/platform/database"
	"github.com/yldm-tech/forma/apps/server/internal/platform/pgtest"
	"github.com/yldm-tech/forma/apps/server/migrations"
)

// Release v0.2.48's `forma import-forma2`, the way the refusal points to for keeping forma2's data, writes to forma2_imports and workspaces.legacy_environment_id, which 00027 drops. `forma migrate` and `forma start` therefore refuse a database with forma2's unread data before applying any migration, and FORMA_IGNORE_FORMA2_DATA=1 still lets `forma migrate` through.
func TestMigrationsWaitForForma2Data(t *testing.T) {
	dsn := pgtest.DSN(t)
	ctx := context.Background()
	provider, cleanup, err := database.NewMigrator(dsn, migrations.FS)
	if err != nil {
		t.Fatalf("NewMigrator: %v", err)
	}
	defer cleanup()
	// The schema of a release before 00027, holding forma2's users and none of this server's.
	if _, err := provider.DownTo(ctx, 26); err != nil {
		t.Fatalf("down to 26: %v", err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(ctx, `CREATE TABLE "User" (id text PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO "User" (id) VALUES ('clx0000000000000000000000')`); err != nil {
		t.Fatal(err)
	}
	version := func() int64 {
		t.Helper()
		v, err := provider.GetDBVersion(ctx)
		if err != nil {
			t.Fatalf("GetDBVersion: %v", err)
		}
		return v
	}

	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("FORMA_IGNORE_FORMA2_DATA", "0")
	if err := migrate(ctx); !errors.Is(err, database.ErrUnreadForma2Data) {
		t.Fatalf("migrate on forma2's unread data = %v, want %v", err, database.ErrUnreadForma2Data)
	}
	if v := version(); v != 26 {
		t.Fatalf("after the refused migrate: schema version %d, want 26", v)
	}

	// start validates the whole configuration first; the refusal comes before its migrations and before anything connects to Valkey.
	t.Setenv("WEBAPP_URL", "http://localhost:3000")
	t.Setenv("REDIS_URL", "redis://127.0.0.1:1")
	t.Setenv("ENCRYPTION_KEY", "a0ff678f48df0da911d2287b6d928688e948dccb2554cdd16f61050fe3d90a86")
	t.Setenv("SKIP_STARTUP_MIGRATION", "")
	if err := start(ctx); !errors.Is(err, database.ErrUnreadForma2Data) {
		t.Fatalf("start on forma2's unread data = %v, want %v", err, database.ErrUnreadForma2Data)
	}
	if v := version(); v != 26 {
		t.Fatalf("after the refused start: schema version %d, want 26", v)
	}

	t.Setenv("FORMA_IGNORE_FORMA2_DATA", "1")
	if err := migrate(ctx); err != nil {
		t.Fatalf("migrate with FORMA_IGNORE_FORMA2_DATA=1: %v", err)
	}
	sources := provider.ListSources()
	if v, last := version(), sources[len(sources)-1].Version; v != last {
		t.Fatalf("after migrate with FORMA_IGNORE_FORMA2_DATA=1: schema version %d, want %d", v, last)
	}
}
