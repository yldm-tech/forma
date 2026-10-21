// Package jobs runs background work on River, a job queue stored in PostgreSQL, so enqueuing a job can share the transaction of the change that caused it. `forma migrate` installs River's tables, `forma worker` works the jobs, and feature modules register their job kinds in a Registry.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/riverqueue/river/rivertype"

	"github.com/yldm-tech/forma/server/internal/platform/config"
)

// Retry and retention follow forma2's BullMQ defaults: three attempts with exponential backoff from five seconds, completed jobs kept for a day and failed ones for a week.
const (
	maxAttempts        = 3
	retryBase          = 5 * time.Second
	completedRetention = 24 * time.Hour
	discardedRetention = 7 * 24 * time.Hour
)

// Migrate installs or upgrades River's own tables (river_job, river_queue, ...). It runs after the goose migrations in `forma migrate`; River versions its schema itself, so these tables are not in server/migrations.
func Migrate(ctx context.Context, databaseURL string, log *slog.Logger) error {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("open job queue migration connection: %w", err)
	}
	defer pool.Close()

	migrator, err := rivermigrate.New(riverpgxv5.New(pool), &rivermigrate.Config{Logger: log})
	if err != nil {
		return fmt.Errorf("load job queue migrations: %w", err)
	}
	res, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
	if err != nil {
		return fmt.Errorf("apply job queue migrations: %w", err)
	}
	for _, v := range res.Versions {
		log.InfoContext(ctx, "job queue migration applied", "version", v.Version, "name", v.Name, "duration", v.Duration)
	}
	if len(res.Versions) == 0 {
		log.InfoContext(ctx, "job queue schema is up to date")
	}
	return nil
}

// OpenPool opens the pgx pool River runs on and checks it with a ping. The returned cleanup closes it.
func OpenPool(ctx context.Context, cfg config.Config) (*pgxpool.Pool, func(), error) {
	pcfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	pcfg.MaxConns = int32(min(cfg.DatabaseMaxOpenConns, 1<<30)) //nolint:gosec // bounded above
	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, nil, fmt.Errorf("open job queue pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, pool.Close, nil
}

// Registry collects the job kinds and periodic jobs a worker runs. It starts with the platform's own system.test-log job; feature modules add theirs in app.NewJobRegistry.
type Registry struct {
	workers  *river.Workers
	periodic []*river.PeriodicJob
}

func NewRegistry(log *slog.Logger) *Registry {
	r := &Registry{workers: river.NewWorkers()}
	Add(r, &TestLogWorker{log: log})
	return r
}

// Add registers the worker for one job kind. Registering a kind twice panics at startup, which is where a wiring mistake should surface.
func Add[T river.JobArgs](r *Registry, w river.Worker[T]) {
	river.AddWorker(r.workers, w)
}

// AddPeriodic schedules a recurring job; its kind must also be registered with Add.
func (r *Registry) AddPeriodic(job *river.PeriodicJob) {
	r.periodic = append(r.periodic, job)
}

// NewInserter returns a client that only enqueues: what `forma serve` and other non-worker processes use. Use InsertTx to enqueue inside the transaction of the change that caused the job.
func NewInserter(pool *pgxpool.Pool, log *slog.Logger) (*river.Client[pgx.Tx], error) {
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{Logger: log, MaxAttempts: maxAttempts})
	if err != nil {
		return nil, fmt.Errorf("create job inserter: %w", err)
	}
	return client, nil
}

// Worker works jobs until its context is cancelled.
type Worker struct {
	client          *river.Client[pgx.Tx]
	shutdownTimeout time.Duration
	log             *slog.Logger
}

func NewWorker(cfg config.Config, pool *pgxpool.Pool, reg *Registry, log *slog.Logger) (*Worker, error) {
	concurrency := cfg.WorkerConcurrency
	if concurrency == 0 {
		concurrency = defaultConcurrency()
	}
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Logger:                      log,
		Workers:                     reg.workers,
		PeriodicJobs:                reg.periodic,
		Queues:                      map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: concurrency}},
		MaxAttempts:                 maxAttempts,
		RetryPolicy:                 exponentialRetry{},
		CompletedJobRetentionPeriod: completedRetention,
		DiscardedJobRetentionPeriod: discardedRetention,
		// On shutdown, running jobs get the same grace period as in-flight HTTP requests before their contexts are cancelled.
		SoftStopTimeout: cfg.ShutdownTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("create job worker: %w", err)
	}
	return &Worker{client: client, shutdownTimeout: cfg.ShutdownTimeout, log: log}, nil
}

// defaultConcurrency is forma2's: every running job can hold a database connection, so jobs take at most half a small pod's pool and never more than four slots.
func defaultConcurrency() int {
	return max(2, min(4, runtime.NumCPU()))
}

// Run works jobs until ctx is cancelled, then stops taking new ones and gives running jobs the shutdown timeout to finish before cancelling them.
func (w *Worker) Run(ctx context.Context) error {
	if err := w.client.Start(context.WithoutCancel(ctx)); err != nil {
		return fmt.Errorf("start job worker: %w", err)
	}
	w.log.InfoContext(ctx, "job worker started")
	<-ctx.Done()

	w.log.Info("job worker shutting down", "timeout", w.shutdownTimeout)
	// The soft stop escalates to cancelling job contexts after SoftStopTimeout; the extra margin lets cancelled jobs return.
	stopCtx, cancel := context.WithTimeout(context.Background(), w.shutdownTimeout+10*time.Second)
	defer cancel()
	if err := w.client.Stop(stopCtx); err != nil {
		return fmt.Errorf("stop job worker: %w", err)
	}
	return nil
}

// exponentialRetry waits 5 s, 10 s, 20 s, ... after successive failures, BullMQ's exponential backoff with forma2's base delay.
type exponentialRetry struct{}

func (exponentialRetry) NextRetry(job *rivertype.JobRow) time.Time {
	attempt := max(job.Attempt, 1)
	return time.Now().UTC().Add(retryBase << min(attempt-1, 20))
}

// TestLogArgs is forma2's system.test-log job: a smoke test that proves a worker is consuming the queue, optionally failing on purpose to exercise retries.
type TestLogArgs struct {
	Message    string         `json:"message"`
	ShouldFail bool           `json:"shouldFail,omitempty"`
	Context    map[string]any `json:"context,omitempty"`
}

func (TestLogArgs) Kind() string { return "system.test-log" }

type TestLogWorker struct {
	river.WorkerDefaults[TestLogArgs]
	log *slog.Logger
}

// errTestLogFailed is the deliberate failure of a test-log job with ShouldFail set.
var errTestLogFailed = errors.New("test log job failed intentionally")

func (w *TestLogWorker) Work(ctx context.Context, job *river.Job[TestLogArgs]) error {
	if job.Args.Message == "" {
		return river.JobCancel(errors.New("test log job needs a message"))
	}
	w.log.DebugContext(ctx, job.Args.Message, "attempt", job.Attempt, "context", job.Args.Context, "job_id", job.ID, "job_kind", job.Kind, "queue", job.Queue)
	if job.Args.ShouldFail {
		return fmt.Errorf("%w: %s", errTestLogFailed, job.Args.Message)
	}
	return nil
}
