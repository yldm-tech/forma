// Command forma is the single Forma binary; the first argument selects the process to run.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/yldm-tech/forma/server/internal/app"
	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/database"
	"github.com/yldm-tech/forma/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/server/internal/platform/logger"
)

const usage = `usage: forma <command>

commands:
  start              the container's default: validate the configuration, migrate unless SKIP_STARTUP_MIGRATION=true, then serve, with the job worker in the same process unless BULLMQ_WORKER_ENABLED=0
  serve              run the HTTP server (API, public survey pages and the admin UI)
  worker             run background jobs and periodic tasks
  migrate            apply pending database migrations (schema, then the job queue), then exit
  wait-for-database  wait until the migration database accepts connections [--timeout-seconds 900] [--interval-seconds 5] [--connection-timeout-seconds 5]
  healthcheck        exit 0 when the server on HTTP_ADDR answers /health (the image's HEALTHCHECK)
  authz              operate the SpiceDB projection: schema, backfill and outbox (forma authz help)
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
	return applyMigrations(ctx, cfg, logger.New(cfg, os.Stdout))
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
	return runServer(ctx, cfg, logger.New(cfg, os.Stdout))
}

func worker(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	return runWorker(ctx, cfg, logger.New(cfg, os.Stdout))
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
