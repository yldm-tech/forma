// Command forma is the single Forma binary; the first argument selects the process to run.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/yldm-tech/forma/server/internal/app"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/database"
	"github.com/yldm-tech/forma/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/server/internal/platform/logger"
)

const usage = `usage: forma <command>

commands:
  serve    run the HTTP server (API, public survey pages and the admin UI)
  worker   run background jobs and periodic tasks
  migrate  apply pending database migrations (schema, then the job queue), then exit
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

	w, cleanup, err := app.InitializeWorker(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer cleanup()

	return w.Run(ctx)
}
