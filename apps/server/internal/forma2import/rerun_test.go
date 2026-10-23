package forma2import_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/yldm-tech/forma/server/internal/forma2import"
	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
)

func withOverwrite(o forma2import.Options) forma2import.Options {
	o.Refresh, o.OverwriteChanges = true, true
	return o
}

func connect(t *testing.T, dsn string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

func importOnce(t *testing.T, dsn string) {
	t.Helper()
	if r, err := forma2import.Run(context.Background(), options(t, dsn)); err != nil {
		t.Fatalf("import: %v\n%s", err, reportText(r))
	}
}

// After the server has changed the imported tables, --refresh would bring back what was deleted here (a revoked API key, a removed member) and overwrite what was changed, so it refuses; --overwrite-changes makes forma2's rows win on purpose.
func TestRefreshRefusesChangesMadeHere(t *testing.T) {
	ctx := context.Background()
	dsn := forma2DB(t, 1)
	importOnce(t, dsn)
	db := openGORM(t, dsn)
	conn := connect(t, dsn)
	revoked := legacyid.ID("clapikeylegacy000000000001")
	exec(t, conn, `DELETE FROM api_keys WHERE id = $1`, revoked)

	r, err := forma2import.Run(ctx, withRefresh(options(t, dsn)))
	if !errors.Is(err, forma2import.ErrChangedSinceImport) || !strings.Contains(err.Error(), "api_keys") {
		t.Fatalf("refresh over a revoked key: %v\n%s", err, reportText(r))
	}
	if n := count(t, db, `SELECT count(*) FROM api_keys WHERE id = ?`, revoked); n != 0 {
		t.Fatal("the refused refresh brought the revoked key back")
	}

	// A tag created here, which forma2 never had.
	exec(t, conn, `INSERT INTO tags (workspace_id, name) VALUES ($1, 'created here')`, workspaceID)
	r, err = forma2import.Run(ctx, withOverwrite(options(t, dsn)))
	if err != nil {
		t.Fatalf("refresh overwriting the changes: %v\n%s", err, reportText(r))
	}
	if n := count(t, db, `SELECT count(*) FROM api_keys WHERE id = ?`, revoked); n != 1 {
		t.Error("--overwrite-changes did not restore forma2's key")
	}
	if n := count(t, db, `SELECT count(*) FROM tags WHERE name = 'created here'`); n != 0 || table(r, "tags", "Tag").Deleted != 1 {
		t.Errorf("--overwrite-changes kept the tag forma2 never had: %+v", table(r, "tags", "Tag"))
	}
	// The refresh is recorded, and the next one finds nothing changed.
	if _, err := forma2import.Run(ctx, withRefresh(options(t, dsn))); err != nil {
		t.Fatalf("refresh after the overwrite: %v", err)
	}
}

// With nothing changed here, --refresh makes the imported tables what forma2 now holds: what forma2 deleted goes, and a newer invite to an address replaces the imported one instead of colliding with it.
func TestRefreshFollowsForma2(t *testing.T) {
	ctx := context.Background()
	dsn := forma2DB(t, 1)
	importOnce(t, dsn)
	conn := connect(t, dsn)
	exec(t, conn, `INSERT INTO "Invite" (id, email, name, "organizationId", "creatorId", "createdAt", "expiresAt", role)
		VALUES ('clinvitenewer000000000000001', 'INVITEE@example.com', 'Ivy', 'clorg00000000000000000001', 'cluserowner0000000000000001', '2026-10-20 10:00:00', '2026-10-27 10:00:00', 'member')`)
	exec(t, conn, `DELETE FROM "Membership" WHERE "userId" = 'clusersso000000000000000001'`)

	r, err := forma2import.Run(ctx, withRefresh(options(t, dsn)))
	if err != nil {
		t.Fatalf("refresh: %v\n%s", err, reportText(r))
	}
	db := openGORM(t, dsn)
	if n := count(t, db, `SELECT count(*) FROM invites WHERE id = ?`, legacyid.ID("clinvitenewer000000000000001")); n != 1 || count(t, db, `SELECT count(*) FROM invites`) != 1 {
		t.Error("the newer invite did not replace the imported one")
	}
	if n := count(t, db, `SELECT count(*) FROM memberships WHERE user_id = ?`, legacyid.ID("clusersso000000000000000001")); n != 0 || table(r, "memberships", "Membership").Deleted != 1 {
		t.Errorf("the membership forma2 removed is still here: %+v", table(r, "memberships", "Membership"))
	}
}

// After a rollback forma2 runs again; a server started on the earlier import would serve without what forma2 wrote since, so the guard refuses until it is imported, or left behind on purpose.
func TestGuardRefusesWhenForma2ChangedSinceTheImport(t *testing.T) {
	ctx := context.Background()
	dsn := forma2DB(t, 1)
	importOnce(t, dsn)
	if err := forma2import.GuardDSN(ctx, dsn); err != nil {
		t.Fatalf("guard after the import: %v", err)
	}
	conn := connect(t, dsn)
	exec(t, conn, `INSERT INTO "User" (id, created_at, updated_at, name, email) VALUES ('clusersignup0000000000000001', now(), now(), 'Signed Up', 'later@example.com')`)
	err := forma2import.GuardDSN(ctx, dsn)
	if !errors.Is(err, forma2import.ErrForma2Changed) || !strings.Contains(err.Error(), `"User"`) {
		t.Fatalf("guard after forma2 took a sign-up: %v", err)
	}
	// Nothing changed here, so a refresh imports it.
	if r, err := forma2import.Run(ctx, withRefresh(options(t, dsn))); err != nil {
		t.Fatalf("refresh: %v\n%s", err, reportText(r))
	}
	if err := forma2import.GuardDSN(ctx, dsn); err != nil {
		t.Fatalf("guard after the refresh: %v", err)
	}

	// forma2 changes again, and so does this server: keeping this server's data and leaving forma2's later writes behind is a recorded decision.
	exec(t, conn, `UPDATE "Survey" SET name = 'Renamed in forma2', updated_at = now() WHERE id = $1`, survey1)
	exec(t, conn, `UPDATE surveys SET name = 'Renamed here' WHERE id = $1`, legacyid.ID(survey1))
	if err := forma2import.GuardDSN(ctx, dsn); !errors.Is(err, forma2import.ErrForma2Changed) {
		t.Fatalf("guard after forma2 changed a survey: %v", err)
	}
	if _, err := forma2import.Run(ctx, withRefresh(options(t, dsn))); !errors.Is(err, forma2import.ErrChangedSinceImport) {
		t.Fatalf("refresh over a change made here: %v", err)
	}
	if err := forma2import.Discard(ctx, dsn, "keep what the Go server holds"); err != nil {
		t.Fatalf("discard forma2's later writes: %v", err)
	}
	if err := forma2import.GuardDSN(ctx, dsn); err != nil {
		t.Fatalf("guard after the discard: %v", err)
	}
	if err := forma2import.Discard(ctx, dsn, "again"); !errors.Is(err, forma2import.ErrAlreadyRecorded) {
		t.Fatalf("a discard with nothing to decide: %v", err)
	}
}
