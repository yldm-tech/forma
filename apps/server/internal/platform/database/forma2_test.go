package database

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"testing"
)

func TestRefuseUnreadForma2Data(t *testing.T) {
	dsn := startPostgres(t)
	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	check := func(want error) {
		t.Helper()
		if err := RefuseUnreadForma2Data(ctx, dsn); !errors.Is(err, want) {
			t.Fatalf("RefuseUnreadForma2Data = %v, want %v", err, want)
		}
	}

	// An empty database, as on a first start before the migrations, passes.
	check(nil)
	// forma2's "User" table with rows and no users table of this server's: refused.
	mustExec(t, db, `CREATE TABLE "User" (id text PRIMARY KEY)`)
	check(nil)
	mustExec(t, db, `INSERT INTO "User" (id) VALUES ('clx0000000000000000000000')`)
	check(ErrUnreadForma2Data)

	if err := MigrateUp(ctx, dsn, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("MigrateUp: %v", err)
	}
	// This server's users table exists but is empty: still refused.
	check(ErrUnreadForma2Data)
	// A user of this server's own, as after an import or a first sign-up: passes.
	mustExec(t, db, `INSERT INTO users (name, email) VALUES ('Ada', 'ada@example.com')`)
	check(nil)
	// Without forma2's rows, an empty users table passes too.
	mustExec(t, db, `DELETE FROM users`)
	mustExec(t, db, `DELETE FROM "User"`)
	check(nil)
	mustExec(t, db, `DROP TABLE "User"`)
	check(nil)
}
