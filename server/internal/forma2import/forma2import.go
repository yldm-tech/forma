// Package forma2import copies a forma2 database (the Next.js and Prisma application this server replaces) into this server's schema: `forma import-forma2`.
//
// forma2's tables and this schema's live side by side in one PostgreSQL database (or the source is another database, --source). The import reads forma2's tables in one read-only, repeatable-read transaction and writes this schema's tables in one transaction of its own, so a run is all or nothing and forma2's tables are never written: its rows stay as they were and forma2 can be started on them again. Every forma2 id becomes legacyid.Resolve of itself, so foreign keys need no mapping table and an old id that arrives from outside resolves to the migrated row.
//
// A run is idempotent. Rows are upserted by their derived primary key: a row that already holds what forma2 has is left alone, a changed one is updated, nothing is duplicated, and nothing is deleted. Before the transaction commits, a verification reads back what was written (every expected row present, sample fields equal to forma2's, every survey document accepted by this server's validators) and a failure rolls the whole run back. The last write of a run records it in forma2_imports, which the server's startup guard and docker/forma.sh wait for.
package forma2import

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/yldm-tech/forma/server/internal/jsonschema"
	"github.com/yldm-tech/forma/server/internal/platform/database"
	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/server/internal/platform/secret"
	"github.com/yldm-tech/forma/server/internal/survey/surveydoc"
	"github.com/yldm-tech/forma/server/migrations"
)

// Options configure one run.
type Options struct {
	// TargetDSN is the database holding this server's schema, migrated to the current version.
	TargetDSN string
	// SourceDSN is forma2's database; empty means the target database, where forma2's tables sit next to this schema's.
	SourceDSN string
	// DryRun does everything a run does, the verification included, and then rolls the transaction back.
	DryRun bool
	// Refresh runs again over a database a previous run (or a discard) already recorded. Migrated rows are then updated to what forma2 holds, which overwrites any change made to them through this server since.
	Refresh bool
	// Cipher seals the two-factor secrets and integration credentials this server stores, under ENCRYPTION_KEY.
	Cipher *secret.Cipher
	// Forma2Key is forma2's ENCRYPTION_KEY, which encrypted its legacy two-factor columns and its integration credentials.
	Forma2Key []byte
	// BetterAuth is the secret Better Auth encrypted forma2's TwoFactor rows with: BETTER_AUTH_SECRET, falling back to NEXTAUTH_SECRET.
	BetterAuth secret.BetterAuthSecrets
	Log        *slog.Logger
}

// TableReport is what a run did for one table of this schema, from one source.
type TableReport struct {
	Table string `json:"table"`
	// From names the forma2 table, or for rows derived from other columns, the column they come from.
	From string `json:"from"`
	// Forma2Rows is the forma2 table's row count, for a table read row by row; -1 for derived rows.
	Forma2Rows int `json:"forma2Rows"`
	Read       int `json:"read"`
	// Skipped rows are dropped on purpose (see SkipReason); the rest are written.
	Skipped    int    `json:"skipped"`
	SkipReason string `json:"skipReason,omitempty"`
	Written    int    `json:"written"`
	Inserted   int    `json:"inserted"`
	Updated    int    `json:"updated"`
	Unchanged  int    `json:"unchanged"`
	// Present is how many of the written rows the verification found in the table.
	Present int `json:"present"`
}

// NotMigrated is a forma2 table the import leaves behind, with why.
type NotMigrated struct {
	Table  string `json:"table"`
	Rows   int    `json:"rows"`
	Reason string `json:"reason"`
}

// Report is the outcome of a run.
type Report struct {
	DryRun    bool `json:"dryRun"`
	Committed bool `json:"committed"`
	// Forma2Migrations are the Prisma migrations the source had applied; production ran the init migration only.
	Forma2Migrations []string `json:"forma2Migrations"`
	// AbsentColumns are columns of forma2's latest schema the source does not have; their rows take the defaults.
	AbsentColumns []string      `json:"absentColumns,omitempty"`
	Tables        []TableReport `json:"tables"`
	NotMigrated   []NotMigrated `json:"notMigrated"`
	// SurveysValidated counts the survey documents checked; SamplesCompared the rows whose fields were compared with forma2's.
	SurveysValidated int `json:"surveysValidated"`
	SamplesCompared  int `json:"samplesCompared"`
	// Failures fail the run; Warnings are reported and the run goes on.
	Failures []string `json:"failures"`
	Warnings []string `json:"warnings"`
}

// ErrVerification is returned, with the report, when the verification failed; the transaction was rolled back.
var ErrVerification = errors.New("verification failed; nothing was written")

// ErrAlreadyRecorded is returned when a previous run or a discard is recorded and Options.Refresh is not set.
var ErrAlreadyRecorded = errors.New("forma2's data has already been imported or discarded here; pass --refresh to import again, which overwrites the migrated rows with forma2's current values")

// chunkSize is how many rows are staged and upserted at a time.
const chunkSize = 2000

// Run imports forma2's database. The report is returned whenever the run got as far as reading forma2, a failed verification included.
func Run(ctx context.Context, opts Options) (*Report, error) {
	if opts.Log == nil {
		opts.Log = slog.New(slog.DiscardHandler)
	}
	started := time.Now()
	if err := schemaCurrent(ctx, opts.TargetDSN); err != nil {
		return nil, err
	}
	target, err := pgx.Connect(ctx, opts.TargetDSN)
	if err != nil {
		return nil, fmt.Errorf("connect to the target database: %w", err)
	}
	defer func() { _ = target.Close(context.WithoutCancel(ctx)) }()
	sourceDSN := opts.SourceDSN
	if sourceDSN == "" {
		sourceDSN = opts.TargetDSN
	}
	source, err := pgx.Connect(ctx, sourceDSN)
	if err != nil {
		return nil, fmt.Errorf("connect to the forma2 database: %w", err)
	}
	defer func() { _ = source.Close(context.WithoutCancel(ctx)) }()

	// forma2's tables are read in a transaction PostgreSQL itself keeps read-only, over one snapshot.
	src, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("open the forma2 snapshot: %w", err)
	}
	defer func() { _ = src.Rollback(context.WithoutCancel(ctx)) }()
	schema, err := readSourceSchema(ctx, src)
	if err != nil {
		return nil, err
	}

	tx, err := target.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("open the import transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := checkTarget(ctx, tx, opts.Refresh); err != nil {
		return nil, err
	}

	schemas, err := jsonschema.New()
	if err != nil {
		return nil, err
	}
	im := &importer{
		opts: opts, src: src, tx: tx, schema: schema, schemas: schemas, docs: surveydoc.NewValidator(schemas),
		report: &Report{DryRun: opts.DryRun, Forma2Migrations: schema.migrations, AbsentColumns: schema.absentColumns(), Failures: []string{}, Warnings: []string{}},
	}
	if err := im.precheck(ctx); err != nil {
		return im.report, err
	}
	for _, spec := range im.tables() {
		if err := im.copyTable(ctx, spec); err != nil {
			return im.report, fmt.Errorf("%s from %s: %w", spec.target, spec.label(), err)
		}
	}
	im.notMigrated()
	if err := im.verify(ctx); err != nil {
		return im.report, err
	}
	if len(im.report.Failures) > 0 {
		return im.report, ErrVerification
	}
	if err := im.record(ctx, started); err != nil {
		return im.report, err
	}
	if opts.DryRun {
		return im.report, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return im.report, fmt.Errorf("commit the import: %w", err)
	}
	im.report.Committed = true
	return im.report, nil
}

// schemaCurrent refuses a target database whose schema is not this release's: the import writes the tables as this release defines them.
func schemaCurrent(ctx context.Context, dsn string) error {
	provider, cleanup, err := database.NewMigrator(dsn, migrations.FS)
	if err != nil {
		return err
	}
	defer cleanup()
	pending, err := provider.HasPending(ctx)
	if err != nil {
		return fmt.Errorf("read the target's schema version: %w", err)
	}
	if pending {
		return errors.New("the target database's schema is not this release's; run `forma migrate` first")
	}
	return nil
}

// importer is one run in progress.
type importer struct {
	opts    Options
	src     pgx.Tx
	tx      pgx.Tx
	schema  *sourceSchema
	schemas *jsonschema.Validator
	docs    *surveydoc.Validator
	report  *Report
	// written holds, per table spec, the keys and samples verification reads back.
	written []*writtenTable
	// existing are the sealed values of the table being written, for its row hook.
	existing sealed
}

func (im *importer) fail(format string, args ...any) {
	im.report.Failures = append(im.report.Failures, fmt.Sprintf(format, args...))
}

func (im *importer) warn(format string, args ...any) {
	im.report.Warnings = append(im.report.Warnings, fmt.Sprintf(format, args...))
}

// checkTarget refuses a second run unless it was asked for.
func checkTarget(ctx context.Context, tx pgx.Tx, refresh bool) error {
	// Serializes concurrent imports: the second waits for the first to commit or roll back, then sees its record.
	if _, err := tx.Exec(ctx, `LOCK TABLE forma2_imports IN EXCLUSIVE MODE`); err != nil {
		return fmt.Errorf("lock forma2_imports: %w", err)
	}
	var recorded bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM forma2_imports)`).Scan(&recorded); err != nil {
		return fmt.Errorf("read forma2_imports: %w", err)
	}
	if recorded && !refresh {
		return ErrAlreadyRecorded
	}
	return nil
}

// record writes the run into forma2_imports, the last write of the transaction.
func (im *importer) record(ctx context.Context, started time.Time) error {
	sourceCounts, err := json.Marshal(im.schema.counts)
	if err != nil {
		return err
	}
	targetCounts := map[string]int{}
	for _, t := range im.report.Tables {
		targetCounts[t.Table] += t.Written
	}
	written, err := json.Marshal(targetCounts)
	if err != nil {
		return err
	}
	if _, err := im.tx.Exec(ctx, `INSERT INTO forma2_imports (started_at, outcome, legacy_namespace, source_counts, target_counts) VALUES ($1, 'imported', $2, $3, $4)`,
		started, legacyid.Namespace, string(sourceCounts), string(written)); err != nil {
		return fmt.Errorf("record the import: %w", err)
	}
	return nil
}
