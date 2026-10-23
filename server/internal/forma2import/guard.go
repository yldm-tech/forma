package forma2import

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrForma2NotImported is the startup guard's refusal.
var ErrForma2NotImported = errors.New(`this database holds forma2's data (its "User" table has rows) and none of it has been imported: a server started now would serve an empty instance whose first sign-up becomes its owner. Run ` +
	"`forma import-forma2` (see the migration guide), or `forma import-forma2 --discard-forma2-data --reason=<why>` to start empty on purpose")

// ErrForma2Changed is the startup guard's refusal of a database where forma2 ran again after the latest import or discard, as it does after a rollback.
var ErrForma2Changed = errors.New("forma2's tables changed after its data was last imported or discarded here (forma2 ran again, as after a rollback): a server started now would serve without what forma2 wrote since. Run " +
	"`forma import-forma2 --refresh` to import it (it refuses when this server changed the imported tables too; `--refresh --overwrite-changes` then makes forma2's rows win), or `forma import-forma2 --discard-forma2-data --reason=<why>` to keep this server's data and leave forma2's later writes behind")

// queryRower is the one call the guard makes.
type queryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Guard refuses a database where forma2's "User" table has rows and forma2_imports records neither an import nor a discard, and a database where forma2's tables changed after the latest record (forma2_import_drift). A database without forma2's tables, or with an empty "User" table and no record, passes. It only reads.
func Guard(ctx context.Context, db queryRower) error {
	var forma2, recorded bool
	if err := db.QueryRow(ctx, `SELECT to_regclass('public."User"') IS NOT NULL, to_regclass('public.forma2_imports') IS NOT NULL`).Scan(&forma2, &recorded); err != nil {
		return fmt.Errorf("check for forma2's data: %w", err)
	}
	if !forma2 {
		return nil
	}
	query := `SELECT EXISTS (SELECT 1 FROM "User"), false`
	if recorded {
		query = `SELECT EXISTS (SELECT 1 FROM "User"), EXISTS (SELECT 1 FROM forma2_imports)`
	}
	var users, hasRecord bool
	if err := db.QueryRow(ctx, query).Scan(&users, &hasRecord); err != nil {
		return fmt.Errorf("check for forma2's data: %w", err)
	}
	if !hasRecord {
		if users {
			return ErrForma2NotImported
		}
		return nil
	}
	changed, err := drift(ctx, db)
	if err != nil {
		return err
	}
	if len(changed) > 0 {
		return fmt.Errorf("%w (changed: %s)", ErrForma2Changed, strings.Join(changed, ", "))
	}
	return nil
}

// drift is forma2_import_drift(): forma2's tables that changed since the latest record, quoted as forma2 names them. A schema older than the function has none.
func drift(ctx context.Context, db queryRower) ([]string, error) {
	var defined bool
	if err := db.QueryRow(ctx, `SELECT to_regprocedure('forma2_import_drift()') IS NOT NULL`).Scan(&defined); err != nil {
		return nil, fmt.Errorf("check forma2's tables for changes: %w", err)
	}
	if !defined {
		return nil, nil
	}
	var tables string
	if err := db.QueryRow(ctx, `SELECT array_to_string(forma2_import_drift(), ',')`).Scan(&tables); err != nil {
		return nil, fmt.Errorf("check forma2's tables for changes: %w", err)
	}
	if tables == "" {
		return nil, nil
	}
	changed := strings.Split(tables, ",")
	for i, t := range changed {
		changed[i] = `"` + t + `"`
	}
	return changed, nil
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

// Discard records that this database starts without forma2's data, or without what forma2 wrote after the latest import or discard, on purpose: the server then starts although forma2's tables hold rows it does not. forma2's tables stay as they are. It refuses when an import or a discard is recorded and forma2's tables have not changed since, as there is nothing to decide.
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
	latest, err := latestRecord(ctx, tx)
	if err != nil {
		return err
	}
	if latest != nil {
		changed, err := drift(ctx, tx)
		if err != nil {
			return err
		}
		if len(changed) == 0 {
			return ErrAlreadyRecorded
		}
	}
	digests, err := targetDigests(ctx, tx)
	if err != nil {
		return err
	}
	digestsJSON, err := json.Marshal(digests)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO forma2_imports (started_at, outcome, reason, target_digests, source_marks) VALUES (now(), 'discarded', $1, $2, forma2_marks())`, reason, string(digestsJSON)); err != nil {
		return fmt.Errorf("record the discard: %w", err)
	}
	return tx.Commit(ctx)
}
