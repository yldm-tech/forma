package forma2import

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"text/tabwriter"

	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/database"
	"github.com/yldm-tech/forma/server/internal/platform/logger"
	"github.com/yldm-tech/forma/server/internal/platform/secret"
)

// CLIUsage documents `forma import-forma2`.
const CLIUsage = `usage: forma import-forma2 [--dry-run] [--source=<postgres url>] [--refresh [--overwrite-changes]]
       forma import-forma2 --discard-forma2-data --reason=<why>

Copies forma2's data (the Next.js release's tables) into this server's schema in one transaction, verifies it and records the import; forma2's tables are only read. Run ` + "`forma migrate`" + ` first.

  --dry-run    import, verify and print the counts, then roll everything back
  --source     forma2's database when it is not the one MIGRATE_DATABASE_URL (or DATABASE_URL) names; forma2's own DATABASE_URL is accepted as it is
  --refresh    import again over an earlier import or discard, making the imported tables what forma2 holds now: changed rows are updated and rows forma2 no longer holds are deleted. Refused when this server changed those tables since the record, as it cannot tell those changes from forma2's
  --overwrite-changes
               with --refresh, also over changes made through this server: forma2's rows win, so rows created here are deleted, rows deleted here (a revoked API key, a removed member) come back and changed rows take forma2's values
  --discard-forma2-data --reason=<why>
               record that this database starts without forma2's data, or without what forma2 wrote after the last import (as after a rollback), so the server starts anyway

Needs ENCRYPTION_KEY and NEXTAUTH_SECRET (or BETTER_AUTH_SECRET) as forma2 had them. With AUTHZED_ENABLED=1 it then rebuilds SpiceDB's relationships (forma authz backfill --apply).
Exit 0 imported (or a clean dry run), 1 failed or not verified (nothing written), 2 misused.
`

type cliFlags struct {
	dryRun, refresh, overwrite, discard bool
	source, reason                      string
}

func parseFlags(args []string) (cliFlags, error) {
	var f cliFlags
	fs := flag.NewFlagSet("import-forma2", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&f.dryRun, "dry-run", false, "")
	fs.BoolVar(&f.refresh, "refresh", false, "")
	fs.BoolVar(&f.overwrite, "overwrite-changes", false, "")
	fs.BoolVar(&f.discard, "discard-forma2-data", false, "")
	fs.StringVar(&f.source, "source", "", "")
	fs.StringVar(&f.reason, "reason", "", "")
	if err := fs.Parse(args); err != nil {
		return f, err
	}
	if fs.NArg() > 0 {
		return f, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if f.overwrite && !f.refresh {
		return f, errors.New("--overwrite-changes goes with --refresh")
	}
	if f.discard && (f.dryRun || f.refresh || f.source != "") {
		return f, errors.New("--discard-forma2-data takes only --reason")
	}
	if !f.discard && f.reason != "" {
		return f, errors.New("--reason goes with --discard-forma2-data")
	}
	if f.source != "" {
		var err error
		if f.source, err = config.SourceDatabaseURL(f.source); err != nil {
			return f, err
		}
	}
	return f, nil
}

// RunCLI runs `forma import-forma2` and returns its exit code.
func RunCLI(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	f, err := parseFlags(args)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "forma import-forma2: %v\n\n%s", err, CLIUsage)
		return 2
	}
	cfg, err := config.LoadForImport()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "forma import-forma2: %v\n", err)
		return 1
	}
	log := logger.New(cfg, stderr)
	if f.discard {
		if err := Discard(ctx, cfg.DatabaseURL, f.reason); err != nil {
			_, _ = fmt.Fprintf(stderr, "forma import-forma2: %v\n", err)
			return 1
		}
		_, _ = fmt.Fprintln(stdout, "Recorded that this server starts without forma2's data as it is now. forma2's tables were not changed.")
		return 0
	}
	cipher, err := secret.NewCipher(cfg)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "forma import-forma2: %v\n", err)
		return 1
	}
	opts := Options{
		TargetDSN: cfg.DatabaseURL, SourceDSN: f.source, DryRun: f.dryRun, Refresh: f.refresh, OverwriteChanges: f.overwrite,
		Cipher: cipher, Forma2Key: cfg.EncryptionKey, BetterAuth: secret.BetterAuthSecrets{Secret: string(cfg.BetterAuthSecret)}, Log: log,
	}
	if opts.BetterAuth.Secret == "" {
		opts.BetterAuth.Secret = string(cfg.NextAuthSecret)
	}
	var report *Report
	// The migration lock keeps the import and a migration (or a second import) from running at once.
	err = database.WithMigrationLock(ctx, cfg.DatabaseURL, log, func(ctx context.Context) error {
		var err error
		report, err = Run(ctx, opts)
		return err
	})
	if report != nil {
		PrintReport(stdout, report)
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "forma import-forma2: %v\n", err)
		return 1
	}
	if !report.Committed {
		return 0
	}
	if !bool(cfg.Authz.Enabled) {
		_, _ = fmt.Fprintln(stdout, "\nSpiceDB: AUTHZED_ENABLED is off here. The import queued the projection of every imported organization, membership, team, workspace and API key; `forma worker` delivers it. Then run `forma authz backfill --apply` where AuthZed is configured.")
		return 0
	}
	return backfill(ctx, cfg, log, stdout, stderr)
}

// backfill rebuilds SpiceDB's relationships from the imported rows, `forma authz backfill --apply` over every organization. It never prunes: relationships it does not know (forma2's, in a SpiceDB both share) are left alone.
func backfill(ctx context.Context, cfg config.Config, log *slog.Logger, stdout, stderr io.Writer) int {
	client, err := authz.NewClient(cfg.Authz, authz.BulkProfile, log)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "forma import-forma2: the import is committed, but SpiceDB is unreachable (%v); run `forma authz backfill --apply`\n", err)
		return 1
	}
	defer func() { _ = client.Close() }()
	db, closeDB, err := database.Open(ctx, cfg, log)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "forma import-forma2: the import is committed, but %v; run `forma authz backfill --apply`\n", err)
		return 1
	}
	defer closeDB()
	res := authz.NewBackfill(db, client, log).Run(ctx, authz.BackfillRequest{Apply: true, MaxPrune: authz.MaxPrune, Scope: authz.BackfillScope{Kind: authz.ScopeAll}})
	_, _ = fmt.Fprintf(stdout, "\nSpiceDB backfill: %s (%d missing written, %d orphaned left alone, %d failed)\n", res.Status, res.Counters.Missing, res.Counters.Orphaned, res.Counters.Failed)
	switch res.Status {
	case authz.StatusReconciled:
		return 0
	case authz.StatusDrifted:
		_, _ = fmt.Fprintln(stdout, "SpiceDB holds relationships no imported row explains (forma2's own, in a SpiceDB both share); they were left alone. `forma authz backfill` reports them.")
		return 0
	default:
		_, _ = fmt.Fprintln(stderr, "forma import-forma2: the import is committed, but the SpiceDB backfill failed; run `forma authz backfill --apply`")
		return 1
	}
}

// PrintReport writes a run's report as text.
func PrintReport(w io.Writer, r *Report) {
	switch {
	case r.Committed:
		_, _ = fmt.Fprintln(w, "forma2 import: committed")
	case r.DryRun && len(r.Failures) == 0:
		_, _ = fmt.Fprintln(w, "forma2 import: dry run, verified and rolled back")
	default:
		_, _ = fmt.Fprintln(w, "forma2 import: not committed")
	}
	if len(r.Forma2Migrations) > 0 {
		_, _ = fmt.Fprintf(w, "forma2 migrations applied: %s\n", strings.Join(r.Forma2Migrations, ", "))
	}
	if len(r.AbsentColumns) > 0 {
		_, _ = fmt.Fprintf(w, "Not in this forma2 database (defaults used): %s\n", strings.Join(r.AbsentColumns, ", "))
	}
	if len(r.Tables) > 0 {
		_, _ = fmt.Fprintln(w)
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
		_, _ = fmt.Fprintln(tw, "table\tfrom\tforma2 rows\tread\tskipped\twritten\tinserted\tupdated\tunchanged\tdeleted\tpresent\t")
		for _, t := range r.Tables {
			forma2 := "-"
			if t.Forma2Rows >= 0 {
				forma2 = fmt.Sprint(t.Forma2Rows)
			}
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t\n", t.Table, t.From, forma2, t.Read, t.Skipped, t.Written, t.Inserted, t.Updated, t.Unchanged, t.Deleted, t.Present)
		}
		_ = tw.Flush()
		for _, t := range r.Tables {
			if t.Skipped > 0 {
				_, _ = fmt.Fprintf(w, "Skipped %d %s rows: %s\n", t.Skipped, t.From, t.SkipReason)
			}
		}
	}
	if len(r.NotMigrated) > 0 {
		_, _ = fmt.Fprintln(w, "\nNot imported:")
		for _, n := range r.NotMigrated {
			_, _ = fmt.Fprintf(w, "  %s (%d rows): %s\n", n.Table, n.Rows, n.Reason)
		}
	}
	_, _ = fmt.Fprintf(w, "\nVerification: %d survey documents validated, %d sample rows compared with forma2's rows read again, %d failures, %d warnings\n",
		r.SurveysValidated, r.SamplesCompared, len(r.Failures), len(r.Warnings))
	for _, f := range r.Failures {
		_, _ = fmt.Fprintf(w, "  FAILED: %s\n", f)
	}
	for _, m := range r.Warnings {
		_, _ = fmt.Fprintf(w, "  warning: %s\n", m)
	}
}
