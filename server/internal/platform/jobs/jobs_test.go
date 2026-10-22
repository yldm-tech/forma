package jobs_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/server/internal/platform/pgtest"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func testConfig(dsn string) config.Config {
	return config.Config{DatabaseURL: dsn, DatabaseMaxOpenConns: 5, ShutdownTimeout: 5 * time.Second, WorkerConcurrency: 2}
}

func openPool(t *testing.T, cfg config.Config) *pgxpool.Pool {
	t.Helper()
	pool, cleanup, err := jobs.OpenPool(context.Background(), cfg)
	if err != nil {
		t.Fatalf("OpenPool: %v", err)
	}
	t.Cleanup(cleanup)
	return pool
}

// startWorker runs the worker until the test ends (or the returned stop is called) and reports Run's result on the channel.
func startWorker(t *testing.T, cfg config.Config, pool *pgxpool.Pool, reg *jobs.Registry) (stop func(), result <-chan error) {
	t.Helper()
	w, err := jobs.NewWorker(cfg, pool, reg, discard)
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		done <- w.Run(ctx)
		close(finished)
	}()
	t.Cleanup(func() {
		cancel()
		<-finished
	})
	return cancel, done
}

func jobState(t *testing.T, pool *pgxpool.Pool, id int64) (state string) {
	t.Helper()
	if err := pool.QueryRow(context.Background(), "SELECT state FROM river_job WHERE id = $1", id).Scan(&state); err != nil {
		t.Fatalf("read job %d: %v", id, err)
	}
	return state
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func insert(t *testing.T, pool *pgxpool.Pool, args river.JobArgs) int64 {
	t.Helper()
	inserter, err := jobs.NewInserter(pool, discard)
	if err != nil {
		t.Fatal(err)
	}
	res, err := inserter.Insert(context.Background(), args, nil)
	if err != nil {
		t.Fatalf("insert %s: %v", args.Kind(), err)
	}
	return res.Job.ID
}

func TestMigrateIsIdempotent(t *testing.T) {
	dsn := pgtest.DSN(t)
	// pgtest's template already ran it once, as `forma migrate` does after goose.
	if err := jobs.Migrate(context.Background(), dsn, discard); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	pool := openPool(t, testConfig(dsn))
	var tables int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM information_schema.tables WHERE table_name IN ('river_job', 'river_queue', 'river_leader', 'river_migration')").Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 4 {
		t.Fatalf("%d of River's tables exist, want 4", tables)
	}
}

func TestWorkerRunsTheTestLogJob(t *testing.T) {
	cfg := testConfig(pgtest.DSN(t))
	pool := openPool(t, cfg)
	startWorker(t, cfg, pool, jobs.NewRegistry(discard))

	id := insert(t, pool, jobs.TestLogArgs{Message: "hello from the test", Context: map[string]any{"k": "v"}})
	waitFor(t, "the job to complete", func() bool {
		state := jobState(t, pool, id)
		return state == "completed"
	})
}

func TestFailingJobIsRetriedWithBackoff(t *testing.T) {
	cfg := testConfig(pgtest.DSN(t))
	pool := openPool(t, cfg)
	startWorker(t, cfg, pool, jobs.NewRegistry(discard))

	id := insert(t, pool, jobs.TestLogArgs{Message: "fail on purpose", ShouldFail: true})
	// River keeps a retry due within a few seconds "available" rather than "retryable", so wait on the recorded error instead of the state.
	waitFor(t, "the first attempt to fail", func() bool {
		var failures int
		if err := pool.QueryRow(context.Background(), "SELECT coalesce(array_length(errors, 1), 0) FROM river_job WHERE id = $1", id).Scan(&failures); err != nil {
			t.Fatal(err)
		}
		return failures >= 1
	})
	var delay time.Duration
	var maxAttempts int
	if err := pool.QueryRow(context.Background(),
		"SELECT scheduled_at - (errors[1]->>'at')::timestamptz, max_attempts FROM river_job WHERE id = $1 AND attempt = 1", id).Scan(&delay, &maxAttempts); err != nil {
		t.Fatal(err)
	}
	if delay < 4*time.Second || delay > 7*time.Second || maxAttempts != 3 {
		t.Fatalf("retry in %v with max attempts %d, want about 5s and 3", delay, maxAttempts)
	}
}

type recordArgs struct {
	Note string `json:"note"`
}

func (recordArgs) Kind() string { return "test.record" }

// recordWorker reports each job it finishes, after holding it for delay.
type recordWorker struct {
	river.WorkerDefaults[recordArgs]
	delay time.Duration
	done  chan string
}

func (w *recordWorker) Work(ctx context.Context, job *river.Job[recordArgs]) error {
	select {
	case <-time.After(w.delay):
	case <-ctx.Done():
		return ctx.Err()
	}
	w.done <- job.Args.Note
	return nil
}

func TestRegistryRunsModuleWorkersAndPeriodicJobs(t *testing.T) {
	cfg := testConfig(pgtest.DSN(t))
	pool := openPool(t, cfg)
	reg := jobs.NewRegistry(discard)
	worker := &recordWorker{done: make(chan string, 4)}
	jobs.Add(reg, worker)
	reg.AddPeriodic(river.NewPeriodicJob(river.PeriodicInterval(time.Hour), func() (river.JobArgs, *river.InsertOpts) {
		return recordArgs{Note: "periodic"}, nil
	}, &river.PeriodicJobOpts{RunOnStart: true}))
	startWorker(t, cfg, pool, reg)

	select {
	case note := <-worker.done:
		if note != "periodic" {
			t.Fatalf("worked %q, want the periodic job", note)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the periodic job never ran")
	}
}

func TestShutdownLetsRunningJobsFinish(t *testing.T) {
	cfg := testConfig(pgtest.DSN(t))
	pool := openPool(t, cfg)
	reg := jobs.NewRegistry(discard)
	worker := &recordWorker{delay: 500 * time.Millisecond, done: make(chan string, 1)}
	jobs.Add(reg, worker)
	stop, result := startWorker(t, cfg, pool, reg)

	id := insert(t, pool, recordArgs{Note: "slow"})
	waitFor(t, "the job to start", func() bool {
		state := jobState(t, pool, id)
		return state == "running"
	})
	stop()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Run = %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
	if state := jobState(t, pool, id); state != "completed" {
		t.Fatalf("job state after shutdown = %q, want completed", state)
	}
}

func TestWorkerFailsToStartWithoutADatabase(t *testing.T) {
	cfg := testConfig("postgres://forma:forma@127.0.0.1:1/forma?sslmode=disable&connect_timeout=1")
	if _, _, err := jobs.OpenPool(context.Background(), cfg); err == nil {
		t.Fatal("OpenPool succeeded against a closed port")
	}
	if err := jobs.Migrate(context.Background(), cfg.DatabaseURL, discard); err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("Migrate = %v, want a connection error", err)
	}
}
