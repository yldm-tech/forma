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

	"github.com/yldm-tech/forma/server/internal/app"
	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/database"
	"github.com/yldm-tech/forma/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/server/internal/platform/logger"
	"github.com/yldm-tech/forma/server/internal/platform/telemetry"
)

const usage = `usage: forma <command>

commands:
  serve    run the HTTP server (API, public survey pages and the admin UI)
  worker   run background jobs and periodic tasks
  migrate  apply pending database migrations (schema, then the job queue), then exit
  authz    operate the SpiceDB projection: schema, backfill and outbox (forma authz help)
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
	case "serve":
		err = serve(ctx)
	case "worker":
		err = worker(ctx)
	case "migrate":
		err = migrate(ctx)
	case "authz":
		if len(args) == 2 && (args[1] == "help" || args[1] == "-h" || args[1] == "--help") {
			fmt.Print(authz.CLIUsage)
			return 0
		}
		return authz.RunCLI(ctx, args[1:], os.Stdout, os.Stderr)
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
	if err := database.MigrateUp(ctx, cfg.DatabaseURL, log); err != nil {
		return err
	}
	return jobs.Migrate(ctx, cfg.DatabaseURL, log)
}

func serve(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := logger.New(cfg, os.Stdout)
	defer startTelemetry(ctx, cfg, log)()

	srv, cleanup, err := app.InitializeServer(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer cleanup()

	return srv.Run(ctx)
}

func worker(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := logger.New(cfg, os.Stdout)
	defer startTelemetry(ctx, cfg, log)()

	w, cleanup, err := app.InitializeWorker(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer cleanup()

	return w.Run(ctx)
}

// telemetryShutdownTimeout bounds the final flush of spans, metrics and error reports.
const telemetryShutdownTimeout = 10 * time.Second

// startTelemetry sets up OpenTelemetry and Sentry before anything else is built, so every instrument and error report of the process goes through them. The returned function flushes both; it runs last, after the process has drained its work.
func startTelemetry(ctx context.Context, cfg config.Config, log *slog.Logger) func() {
	otel := telemetry.Setup(ctx, cfg, log)
	flushSentry := telemetry.SetupSentry(ctx, cfg, log)
	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), telemetryShutdownTimeout)
		defer cancel()
		otel.Shutdown(shutdownCtx)
		flushSentry()
	}
}
