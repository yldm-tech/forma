package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/database"
	"github.com/yldm-tech/forma/apps/server/internal/platform/logger"
)

// startupMigrationTimeout bounds the migrations `forma start` runs before serving, the timeout forma2's entrypoint gave them.
const startupMigrationTimeout = 300 * time.Second

// start is forma2's container entrypoint (scripts/docker/next-start.sh) for the single binary: the whole configuration is validated before anything touches the database, the migrations run unless a separate job owns them, and then the process serves. forma2 ran its job worker inside the web process, so start does too unless BULLMQ_WORKER_ENABLED=0; Helm and docker compose run `serve` and `worker` as separate processes instead.
func start(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	startup, err := config.LoadStartup()
	if err != nil {
		return err
	}
	log := logger.New(cfg, os.Stdout)
	log.InfoContext(ctx, "environment variables validated")
	// One telemetry setup covers the server and the worker sharing this process.
	defer startTelemetry(ctx, cfg, log)()

	// Whether this process migrates is logged, and the migration configuration validated, before anything touches the database.
	var migrateCfg config.Config
	if startup.SkipMigration {
		log.InfoContext(ctx, "skipping startup migrations (SKIP_STARTUP_MIGRATION is set; a migration job applies them)")
	} else if migrateCfg, err = config.LoadForMigrate(); err != nil {
		return err
	}

	// A database that holds only forma2's data would come up as an empty instance; refuse unless told to start empty. Checked before the migrations, as `forma migrate` does, so none of them runs on such a database: a later one may drop what release v0.2.48's `forma import-forma2` writes to.
	if err := refuseUnreadForma2Data(ctx, cfg, log); err != nil {
		return err
	}

	if !startup.SkipMigration {
		log.InfoContext(ctx, "running database migrations", "timeout", startupMigrationTimeout)
		migrateCtx, cancel := context.WithTimeout(ctx, startupMigrationTimeout)
		err = applyMigrations(migrateCtx, migrateCfg, log)
		cancel()
		if err != nil {
			return fmt.Errorf("startup migrations: %w", err)
		}
	}

	if !startup.WorkerEnabled {
		log.InfoContext(ctx, "starting the server without the job worker (BULLMQ_WORKER_ENABLED=0)")
		return runServer(ctx, cfg, log)
	}
	log.InfoContext(ctx, "starting the server and the job worker")
	// Either one failing stops the other, so the container exits and is restarted as a whole.
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return runServer(gctx, cfg, log) })
	g.Go(func() error { return runWorker(gctx, cfg, log) })
	return g.Wait()
}

// waitForDatabase is forma2's wait-for-database script with its flags: the Helm migration job's init container runs it before `forma migrate`.
func waitForDatabase(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("wait-for-database", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	timeout := flags.Float64("timeout-seconds", 900, "")
	interval := flags.Float64("interval-seconds", 5, "")
	connectionTimeout := flags.Float64("connection-timeout-seconds", 5, "")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	for name, v := range map[string]float64{"--timeout-seconds": *timeout, "--interval-seconds": *interval, "--connection-timeout-seconds": *connectionTimeout} {
		if !(v > 0) {
			return fmt.Errorf("%s must be a positive number", name)
		}
	}

	cfg, err := config.LoadForMigrate()
	if err != nil {
		return err
	}
	opts := database.WaitOptions{Timeout: seconds(*timeout), Interval: seconds(*interval), ConnectionTimeout: seconds(*connectionTimeout)}
	return database.WaitReachable(ctx, cfg.DatabaseURL, opts, logger.New(cfg, os.Stdout))
}

func seconds(v float64) time.Duration { return time.Duration(v * float64(time.Second)) }

// healthcheck probes the local server's /health, which answers without touching any dependency, so a container is unhealthy only when the process stops serving.
func healthcheck(ctx context.Context) error {
	cfg, err := config.LoadForHealthcheck()
	if err != nil {
		return err
	}
	target, err := healthURL(cfg.HTTPAddr)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("GET /health answered %d", res.StatusCode)
	}
	return nil
}

// healthURL is where the server listening on addr (HTTP_ADDR) answers from inside its own container: the configured host, or loopback when it listens on every interface.
func healthURL(addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("HTTP_ADDR %q: %w", addr, err)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return "", errors.New("HTTP_ADDR must name a numeric port")
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/health", nil
}
