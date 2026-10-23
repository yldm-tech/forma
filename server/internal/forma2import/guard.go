package forma2import

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrForma2NotImported is the startup guard's refusal.
var ErrForma2NotImported = errors.New(`this database holds forma2's data (its "User" table has rows) and none of it has been imported: a server started now would serve an empty instance whose first sign-up becomes its owner. Run ` +
	"`forma import-forma2` (see the migration guide), or `forma import-forma2 --discard-forma2-data --reason=<why>` to start empty on purpose")

// queryRower is the one call the guard makes.
type queryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Guard refuses a database where forma2's "User" table has rows and forma2_imports records neither an import nor a discard. A database without forma2's tables, or with an empty "User" table, passes. It only reads.
func Guard(ctx context.Context, db queryRower) error {
	var forma2, recorded bool
	if err := db.QueryRow(ctx, `SELECT to_regclass('public."User"') IS NOT NULL, to_regclass('public.forma2_imports') IS NOT NULL`).Scan(&forma2, &recorded); err != nil {
		return fmt.Errorf("check for forma2's data: %w", err)
	}
	if !forma2 {
		return nil
	}
	query := `SELECT EXISTS (SELECT 1 FROM "User")`
	if recorded {
		query = `SELECT EXISTS (SELECT 1 FROM "User") AND NOT EXISTS (SELECT 1 FROM forma2_imports)`
	}
	var refuse bool
	if err := db.QueryRow(ctx, query).Scan(&refuse); err != nil {
		return fmt.Errorf("check for forma2's data: %w", err)
	}
	if refuse {
		return ErrForma2NotImported
	}
	return nil
}

// GuardDSN runs Guard on its own short connection. It uses the simple query protocol, so it works through a transaction-pooling PgBouncer as well.
func GuardDSN(ctx context.Context, dsn string) error {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("check for forma2's data: %w", err)
	}
	cfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("check for forma2's data: %w", err)
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	return Guard(ctx, conn)
}

// Discard records that this database starts without forma2's data, on purpose: the server then starts although forma2's "User" table has rows. forma2's tables stay as they are. It refuses when an import or a discard is already recorded.
func Discard(ctx context.Context, dsn, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return errors.New("--discard-forma2-data needs --reason=<why>, which is recorded")
	}
	if err := schemaCurrent(ctx, dsn); err != nil {
		return err
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect to the target database: %w", err)
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := checkTarget(ctx, tx, false); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO forma2_imports (started_at, outcome, reason) VALUES (now(), 'discarded', $1)`, reason); err != nil {
		return fmt.Errorf("record the discard: %w", err)
	}
	return tx.Commit(ctx)
}
