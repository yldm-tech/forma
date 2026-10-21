package auth

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/yldm-tech/forma/server/internal/platform/pgtest"
	"github.com/yldm-tech/forma/server/internal/platform/secret"
)

func TestCleanupDeletesOnlyExpiredRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pgtest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	var user string
	if err := pool.QueryRow(ctx, `INSERT INTO users (name, email) VALUES ('Ada', 'ada@example.com') RETURNING id`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	past, future := time.Now().Add(-time.Minute), time.Now().Add(time.Hour)
	exec(`INSERT INTO sessions (user_id, token_hash, expires_at) VALUES ($1, $2, $3), ($1, $4, $5)`, user, secret.HashToken("old"), past, secret.HashToken("live"), future)
	exec(`INSERT INTO verifications (purpose, user_id, email, token_hash, expires_at) VALUES ('password_reset', $1, 'ada@example.com', $2, $3), ('password_reset', $1, 'ada@example.com', $4, $5)`,
		user, secret.HashToken("old"), past, secret.HashToken("live"), future)

	w := &CleanupWorker{pool: pool, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err := w.Work(ctx, &river.Job[CleanupArgs]{}); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"sessions", "verifications"} {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE token_hash = $1`, secret.HashToken("live")).Scan(&n); err != nil || n != 1 {
			t.Errorf("%s: live row count %d, %v", table, n, err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil || n != 1 {
			t.Errorf("%s: %d rows left, want 1", table, n)
		}
	}
}
