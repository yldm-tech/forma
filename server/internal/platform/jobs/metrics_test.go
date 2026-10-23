package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func collect(t *testing.T, reader *sdkmetric.ManualReader) map[string]metricdata.Aggregation {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &rm); err != nil {
		t.Fatal(err)
	}
	out := map[string]metricdata.Aggregation{}
	for _, sm := range rm.ScopeMetrics {
		if sm.Scope.Name != "forma.jobs" {
			t.Errorf("instrument scope %q, want forma.jobs", sm.Scope.Name)
		}
		for _, m := range sm.Metrics {
			out[m.Name] = m.Data
		}
	}
	return out
}

func sumOf(t *testing.T, data metricdata.Aggregation, attrs ...attribute.KeyValue) int64 {
	t.Helper()
	sum, ok := data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("%T is not an int64 sum", data)
	}
	want := attribute.NewSet(attrs...)
	for _, dp := range sum.DataPoints {
		if dp.Attributes.Equals(&want) {
			return dp.Value
		}
	}
	return 0
}

func histogramCount(t *testing.T, data metricdata.Aggregation, attrs ...attribute.KeyValue) (uint64, []float64) {
	t.Helper()
	h, ok := data.(metricdata.Histogram[float64])
	if !ok {
		t.Fatalf("%T is not a float64 histogram", data)
	}
	want := attribute.NewSet(attrs...)
	for _, dp := range h.DataPoints {
		if dp.Attributes.Equals(&want) {
			return dp.Count, dp.Bounds
		}
	}
	return 0, nil
}

func TestWorkerMiddlewareRecordsOutcomesDurationsAndFirstAttemptWaits(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	m := &metricsMiddleware{metrics: newJobMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))}
	scheduled := time.Now().Add(-3 * time.Second)
	attempted := time.Now()
	job := func(attempt int) *rivertype.JobRow {
		return &rivertype.JobRow{Kind: "response-pipeline.process", Attempt: attempt, ScheduledAt: scheduled, AttemptedAt: &attempted}
	}
	ctx := context.Background()
	boom := errors.New("boom")

	if err := m.Work(ctx, job(1), func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := m.Work(ctx, job(1), func(context.Context) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("Work returned %v, want the job's own error", err)
	}
	// The retry's wait is the backoff, not a backlog: it is not a wait observation.
	if err := m.Work(ctx, job(2), func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	// A snoozed job has not settled.
	if err := m.Work(ctx, job(1), func(context.Context) error { return river.JobSnooze(time.Minute) }); err == nil {
		t.Fatal("the snooze was swallowed")
	}

	got := collect(t, reader)
	name := attribute.String("job_name", "response-pipeline.process")
	completed, failed := attribute.String("status", "completed"), attribute.String("status", "failed")
	if n := sumOf(t, got["forma_jobs_processed_total"], name, completed); n != 2 {
		t.Errorf("completed = %d, want 2", n)
	}
	if n := sumOf(t, got["forma_jobs_processed_total"], name, failed); n != 1 {
		t.Errorf("failed = %d, want 1", n)
	}
	if n, bounds := histogramCount(t, got["forma_jobs_duration_seconds"], name, completed); n != 2 || len(bounds) != 12 || bounds[0] != 0.05 || bounds[11] != 300 {
		t.Errorf("duration observations = %d with bounds %v, want 2 on forma2's buckets", n, bounds)
	}
	if n, bounds := histogramCount(t, got["forma_jobs_wait_duration_seconds"], name); n != 3 || len(bounds) != 13 || bounds[12] != 600 {
		t.Errorf("wait observations = %d with bounds %v, want the 3 first attempts on forma2's buckets", n, bounds)
	}
	h := got["forma_jobs_wait_duration_seconds"].(metricdata.Histogram[float64])
	if dp := h.DataPoints[0]; dp.Sum < 3*2.9 {
		t.Errorf("wait sum = %v s, want about three times three seconds", dp.Sum)
	}
}

func TestEnqueueOutcomes(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	m := newJobMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	m.recordEnqueue(t.Context(), "workflow-run.process", nil)
	m.recordEnqueue(t.Context(), "workflow-run.process", nil)
	m.recordEnqueue(t.Context(), "workflow-run.process", errors.New("insert failed"))

	got := collect(t, reader)
	name := attribute.String("job_name", "workflow-run.process")
	if n := sumOf(t, got["forma_jobs_enqueue_total"], name, attribute.String("status", "enqueued")); n != 2 {
		t.Errorf("enqueued = %d, want 2", n)
	}
	if n := sumOf(t, got["forma_jobs_enqueue_total"], name, attribute.String("status", "failed")); n != 1 {
		t.Errorf("failed = %d, want 1", n)
	}
}
