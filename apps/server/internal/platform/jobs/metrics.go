package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// jobMetrics are forma2's background job instruments (apps/web/lib/jobs/metrics.ts), under its meter, names and buckets. Their only attributes are the job kind, a closed set registered in code, the queue the worker took it from, the closed set of jobs.go, and the outcome. The wait histogram is the backlog signal: how long a job waited past the time it was due before a worker took it, which by queue shows which one is behind.
type jobMetrics struct {
	processed metric.Int64Counter
	duration  metric.Float64Histogram
	wait      metric.Float64Histogram
	enqueued  metric.Int64Counter
}

func newJobMetrics(mp metric.MeterProvider) *jobMetrics {
	meter := mp.Meter("forma.jobs")
	m := &jobMetrics{}
	// Creating an instrument only fails on an invalid name or option, which these constants are not; a failure still yields a usable no-op instrument.
	m.processed, _ = meter.Int64Counter("forma_jobs_processed_total", metric.WithDescription("Background jobs that settled, by job name and outcome"))
	m.duration, _ = meter.Float64Histogram("forma_jobs_duration_seconds", metric.WithDescription("Duration of a background job handler"), metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300))
	m.wait, _ = meter.Float64Histogram("forma_jobs_wait_duration_seconds", metric.WithDescription("Time a background job waited past its intended run time before a worker picked it up"), metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.05, 0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600))
	m.enqueued, _ = meter.Int64Counter("forma_jobs_enqueue_total", metric.WithDescription("Background job enqueue attempts, by job name and outcome"))
	return m
}

// recordEnqueue counts one enqueue attempt. A failed one is the only trace a lost job leaves when its caller does not fail with it.
func (m *jobMetrics) recordEnqueue(ctx context.Context, kind string, err error) {
	status := "enqueued"
	if err != nil {
		status = "failed"
	}
	m.enqueued.Add(ctx, 1, metric.WithAttributes(attribute.String("job_name", kind), attribute.String("status", status)))
}

// metricsMiddleware records every worked attempt: its outcome and handler duration, and on the first attempt how long the job waited past its scheduled time. Retries are left out of the wait histogram because their wait is the retry backoff, not a backlog.
type metricsMiddleware struct {
	river.MiddlewareDefaults
	metrics *jobMetrics
}

func (m *metricsMiddleware) Work(ctx context.Context, job *rivertype.JobRow, doInner func(context.Context) error) error {
	started := time.Now()
	name, queue := attribute.String("job_name", job.Kind), attribute.String("queue", job.Queue)
	if job.Attempt <= 1 && job.AttemptedAt != nil {
		m.metrics.wait.Record(ctx, max(0, job.AttemptedAt.Sub(job.ScheduledAt).Seconds()), metric.WithAttributes(name, queue))
	}
	err := doInner(ctx)
	var snooze *rivertype.JobSnoozeError
	if errors.As(err, &snooze) {
		// A snoozed job has not settled; it is worked again later.
		return err
	}
	status := attribute.String("status", "completed")
	if err != nil {
		status = attribute.String("status", "failed")
	}
	attrs := metric.WithAttributes(name, queue, status)
	m.metrics.processed.Add(ctx, 1, attrs)
	m.metrics.duration.Record(ctx, time.Since(started).Seconds(), attrs)
	return err
}

// defaultJobMetrics records through the global meter provider, which telemetry.Setup installs when an exporter is configured.
func defaultJobMetrics() *jobMetrics {
	return newJobMetrics(otel.GetMeterProvider())
}
