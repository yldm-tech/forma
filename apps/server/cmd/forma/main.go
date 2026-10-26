// Command forma is the single Forma binary; the first argument selects the process to run.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yldm-tech/forma/apps/server/internal/app"
	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/analytics"
	"github.com/yldm-tech/forma/apps/server/internal/platform/buildinfo"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/database"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/apps/server/internal/platform/logger"
	"github.com/yldm-tech/forma/apps/server/internal/platform/telemetry"
)

const usage = `usage: forma <command>

commands:
  start              the container's default: validate the configuration, migrate unless SKIP_STARTUP_MIGRATION=true, then serve, with the job worker in the same process unless WORKER_ENABLED=0
  serve              run the HTTP server (API, public survey pages and the admin UI)
  worker             run background jobs and periodic tasks
  migrate            apply pending database migrations (schema, then the job queue), then exit
  wait-for-database  wait until the migration database accepts connections [--timeout-seconds 900] [--interval-seconds 5] [--connection-timeout-seconds 5]
  healthcheck        exit 0 when the server on HTTP_ADDR answers /health (the image's HEALTHCHECK)
  authz              operate the SpiceDB projection: schema, backfill and outbox (forma authz help)
  seed               write the demo organization, users, surveys and responses; needs ALLOW_SEED=1 [--clear]
  user               turn an account on or off on the whole instance: forma user activate|deactivate <email> (forma user help)
  version            print the release version the binary was built as (0.0.0 for a development build)
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) < 1 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch args[0] {
	case "start":
		err = start(ctx)
	case "serve":
		err = serve(ctx)
	case "worker":
		err = worker(ctx)
	case "migrate":
		err = migrate(ctx)
	case "wait-for-database":
		err = waitForDatabase(ctx, args[1:])
	case "healthcheck":
		err = healthcheck(ctx)
	case "authz":
		if len(args) == 2 && (args[1] == "help" || args[1] == "-h" || args[1] == "--help") {
			fmt.Print(authz.CLIUsage)
			return 0
		}
		return authz.RunCLI(ctx, args[1:], os.Stdout, os.Stderr)
	case "seed":
		err = runSeed(ctx, args[1:])
	case "user":
		if len(args) == 2 && (args[1] == "help" || args[1] == "-h" || args[1] == "--help") {
			fmt.Print(userUsage)
			return 0
		}
		err = runUser(ctx, args[1:], os.Stdout)
	case "version":
		fmt.Println(buildinfo.Version)
		return 0
	case "-h", "--help", "help":
		fmt.Print(usage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "forma %s: %v\n", args[0], err)
		return 1
	}
	return 0
}

func migrate(ctx context.Context) error {
	cfg, err := config.LoadForMigrate()
	if err != nil {
		return err
	}
	log := logger.New(cfg, os.Stdout)
	// Checked before any migration: a later one may drop what release v0.2.48's `forma import-forma2` writes to (00027 does), so a database whose forma2 data was never imported keeps a schema that import still accepts.
	if err := refuseUnreadForma2Data(ctx, cfg, log); err != nil {
		return err
	}
	return applyMigrations(ctx, cfg, log)
}

// applyMigrations runs the goose schema migrations, then River's, holding the migration lock so concurrent migrators take turns.
func applyMigrations(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	return database.WithMigrationLock(ctx, cfg.DatabaseURL, log, func(ctx context.Context) error {
		if err := database.MigrateUp(ctx, cfg.DatabaseURL, log); err != nil {
			return err
		}
		return jobs.Migrate(ctx, cfg.DatabaseURL, log)
	})
}

func serve(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := logger.New(cfg, os.Stdout)
	defer startTelemetry(ctx, cfg, log)()
	if err := refuseUnreadForma2Data(ctx, cfg, log); err != nil {
		return err
	}
	return runServer(ctx, cfg, log)
}

func worker(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := logger.New(cfg, os.Stdout)
	defer startTelemetry(ctx, cfg, log)()
	if err := refuseUnreadForma2Data(ctx, cfg, log); err != nil {
		return err
	}
	return runWorker(ctx, cfg, log)
}

// refuseUnreadForma2Data stops serve, worker, start and migrate on a database that holds forma2's data and no user of this server's, unless FORMA_IGNORE_FORMA2_DATA is set: started there, the server would come up empty and admit its first sign-up as administrator.
func refuseUnreadForma2Data(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	if cfg.IgnoreForma2Data {
		log.WarnContext(ctx, "not checking the database for forma2's data (FORMA_IGNORE_FORMA2_DATA is set); forma2's tables are not read")
		return nil
	}
	return database.RefuseUnreadForma2Data(ctx, cfg.DatabaseURL)
}

func runServer(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	srv, cleanup, err := app.InitializeServer(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer cleanup()

	return srv.Run(ctx)
}

func runWorker(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	w, cleanup, err := app.InitializeWorker(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer cleanup()

	return w.Run(ctx)
}

// telemetryShutdownTimeout bounds the final flush of spans, metrics and error reports.
const telemetryShutdownTimeout = 10 * time.Second

// startTelemetry sets up OpenTelemetry, Sentry and PostHog before anything else is built, so every instrument, error report and product event of the process goes through them. The returned function flushes all three; it runs last, after the process has drained its work.
func startTelemetry(ctx context.Context, cfg config.Config, log *slog.Logger) func() {
	otel := telemetry.Setup(ctx, cfg, log)
	flushSentry := telemetry.SetupSentry(ctx, cfg, log)
	closeAnalytics, err := analytics.Setup(cfg, log)
	if err != nil {
		log.ErrorContext(ctx, "PostHog could not be set up; product analytics are off", "err", err)
		closeAnalytics = func() {}
	}
	return func() {
		closeAnalytics()
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), telemetryShutdownTimeout)
		defer cancel()
		otel.Shutdown(shutdownCtx)
		flushSentry()
	}
}
