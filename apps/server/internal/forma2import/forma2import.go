// Package forma2import copies a forma2 database (the Next.js and Prisma application this server replaces) into this server's schema: `forma import-forma2`.
//
// forma2's tables and this schema's live side by side in one PostgreSQL database (or the source is another database, --source). The import reads forma2's tables in one read-only, repeatable-read transaction and writes this schema's tables in one transaction of its own, so a run is all or nothing and forma2's tables are never written: its rows stay as they were and forma2 can be started on them again. Every forma2 id becomes legacyid.Resolve of itself, so foreign keys need no mapping table and an old id that arrives from outside resolves to the migrated row.
//
// A run is idempotent. Rows are upserted by their derived primary key: a row that already holds what forma2 has is left alone, a changed one is updated, and nothing is duplicated. Before the transaction commits, a verification checks what was written against forma2 read again on its own terms (every forma2 row is in the table under the id legacyid derives from its own key, sampled rows hold what verification computes from forma2's stored columns apart from the mapping, every survey document is accepted by this server's validators) and a failure rolls the whole run back. The last write of a run records it in forma2_imports, with a digest of the tables written and marks of forma2's tables: the server's startup guard and deploy/docker/forma.sh wait for the record and refuse when forma2 ran again after it, and a refresh refuses when this server changed the tables since.
package forma2import

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/yldm-tech/forma/apps/server/internal/jsonschema"
	"github.com/yldm-tech/forma/apps/server/internal/platform/database"
	"github.com/yldm-tech/forma/apps/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
	"github.com/yldm-tech/forma/apps/server/migrations"
)

// Options configure one run.
type Options struct {
	// TargetDSN is the database holding this server's schema, migrated to the current version.
	TargetDSN string
	// SourceDSN is forma2's database; empty means the target database, where forma2's tables sit next to this schema's.
	SourceDSN string
	// DryRun does everything a run does, the verification included, and then rolls the transaction back.
	DryRun bool
	// Refresh runs again over a database a previous run (or a discard) already recorded, and makes the imported tables what forma2 holds now: changed rows are updated, and rows forma2 no longer holds are deleted. It refuses (ErrChangedSinceImport) when this server has changed those tables since the record, unless OverwriteChanges is set.
	Refresh bool
	// OverwriteChanges lets a refresh run over changes made through this server since the record, and makes forma2's rows win: rows created through this server are deleted, rows deleted through it (a revoked API key, a removed member) come back, and changed rows take forma2's values.
	OverwriteChanges bool
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
	// Deleted is how many rows of the table a refresh deleted because the run did not write them, on the table's first report.
	Deleted int `json:"deleted"`
	// Present is how many rows the verification found in the table: for a table read row by row, of forma2's rows read again by their own keys; for derived rows, of the rows written.
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

// ErrAlreadyRecorded is returned when a previous run or a discard is recorded and Options.Refresh is not set, and by a discard with nothing to decide.
var ErrAlreadyRecorded = errors.New("forma2's data has already been imported or discarded here; --refresh imports it again while this server has not changed the imported tables since")

// ErrChangedSinceImport is returned by a refresh when this server changed the imported tables since the latest record, and Options.OverwriteChanges is not set.
var ErrChangedSinceImport = errors.New("this server changed the imported tables since forma2's data was last imported or discarded here, and a refresh cannot tell those changes from forma2's: it would bring back rows deleted here (a revoked API key, a removed member), delete rows created here and overwrite what was changed. Pass --overwrite-changes as well to make forma2's rows win anyway; to keep this server's data instead, do not refresh (if forma2 ran again since, record that with --discard-forma2-data --reason=<why>)")

// Run imports forma2's database. The report is returned whenever the run got as far as reading forma2, a failed verification included.
func Run(ctx context.Context, opts Options) (*Report, error) {
	return run(ctx, opts, nil)
}

// run is Run; adjust, when set, sees the table specs before they are read.
func run(ctx context.Context, opts Options, adjust func([]*tableSpec)) (*Report, error) {
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
	if err := checkTarget(ctx, tx, opts); err != nil {
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
	specs := im.tables()
	if adjust != nil {
		adjust(specs)
	}
	for i, spec := range specs {
		if err := im.stageTable(ctx, i, spec); err != nil {
			return im.report, fmt.Errorf("%s from %s: %w", spec.target, spec.label(), err)
		}
	}
	if opts.Refresh {
		if err := im.prune(ctx); err != nil {
			return im.report, err
		}
	}
	for _, w := range im.written {
		if err := im.writeTable(ctx, w); err != nil {
			return im.report, fmt.Errorf("%s from %s: %w", w.spec.target, w.spec.label(), err)
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

// record is the latest row of forma2_imports.
type record struct {
	completedAt time.Time
	// digests are its target_digests; nil when it has none.
	digests map[string]string
}

// latestRecord locks forma2_imports and reads its latest row; nil when it is empty. The lock serializes imports and discards: the second waits for the first to commit or roll back, then sees its record.
func latestRecord(ctx context.Context, tx pgx.Tx) (*record, error) {
	if _, err := tx.Exec(ctx, `LOCK TABLE forma2_imports IN EXCLUSIVE MODE`); err != nil {
		return nil, fmt.Errorf("lock forma2_imports: %w", err)
	}
	var r record
	var digests []byte
	err := tx.QueryRow(ctx, `SELECT completed_at, target_digests FROM forma2_imports ORDER BY completed_at DESC, id DESC LIMIT 1`).Scan(&r.completedAt, &digests)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read forma2_imports: %w", err)
	}
	if digests != nil {
		if err := json.Unmarshal(digests, &r.digests); err != nil {
			return nil, fmt.Errorf("read forma2_imports: %w", err)
		}
	}
	return &r, nil
}

// checkTarget refuses a second run unless it was asked for, and a refresh over changes this server made since the latest record unless those are to be overwritten.
func checkTarget(ctx context.Context, tx pgx.Tx, opts Options) error {
	latest, err := latestRecord(ctx, tx)
	if err != nil || latest == nil {
		return err
	}
	if !opts.Refresh {
		return ErrAlreadyRecorded
	}
	if opts.OverwriteChanges {
		return nil
	}
	if latest.digests == nil {
		return fmt.Errorf("%w (the record of %s predates the digests a refresh compares)", ErrChangedSinceImport, latest.completedAt.UTC().Format(time.RFC3339))
	}
	current, err := targetDigests(ctx, tx)
	if err != nil {
		return err
	}
	var changed []string
	for _, table := range slices.Sorted(maps.Keys(targetTables)) {
		if current[table] != latest.digests[table] {
			changed = append(changed, table)
		}
	}
	if len(changed) > 0 {
		return fmt.Errorf("%w (changed since the record of %s: %s)", ErrChangedSinceImport, latest.completedAt.UTC().Format(time.RFC3339), strings.Join(changed, ", "))
	}
	return nil
}

// targetDigests fingerprints every table the import writes: its row count and the sum of a 64-bit hash of each row's text, which changes when a row is inserted, deleted or updated. The settings that shape a row's text are fixed for the transaction, so a digest does not depend on the session that takes it.
func targetDigests(ctx context.Context, tx pgx.Tx) (map[string]string, error) {
	if _, err := tx.Exec(ctx, `SET LOCAL TimeZone = 'UTC'; SET LOCAL DateStyle = 'ISO, YMD'; SET LOCAL IntervalStyle = 'postgres'; SET LOCAL extra_float_digits = 1; SET LOCAL bytea_output = 'hex'`); err != nil {
		return nil, fmt.Errorf("digest the imported tables: %w", err)
	}
	out := map[string]string{}
	for _, table := range slices.Sorted(maps.Keys(targetTables)) {
		var digest string
		if err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT count(*)::text || ':' || COALESCE(sum(hashtextextended(t::text, 0)), 0)::text FROM %s t`, pgx.Identifier{table}.Sanitize())).Scan(&digest); err != nil {
			return nil, fmt.Errorf("digest %s: %w", table, err)
		}
		out[table] = digest
	}
	return out, nil
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
	digests, err := targetDigests(ctx, im.tx)
	if err != nil {
		return err
	}
	digestsJSON, err := json.Marshal(digests)
	if err != nil {
		return err
	}
	if _, err := im.tx.Exec(ctx, `INSERT INTO forma2_imports (started_at, outcome, legacy_namespace, source_counts, target_counts, target_digests, source_marks) VALUES ($1, 'imported', $2, $3, $4, $5, $6)`,
		started, legacyid.Namespace, string(sourceCounts), string(written), string(digestsJSON), im.schema.marks); err != nil {
		return fmt.Errorf("record the import: %w", err)
	}
	return nil
}
