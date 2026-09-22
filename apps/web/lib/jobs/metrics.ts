import "server-only";
import { metrics } from "@opentelemetry/api";
import { type JobsObserver, setJobsObserver } from "@forma/jobs";

/**
 * Operational metrics for the background job queue.
 *
 * The queue carries every survey response, webhook, follow-up email, integration write, workflow run and
 * billing event, and until this existed it emitted nothing an operator could alert on. The gap that
 * mattered is a *backlog*: with a worker concurrency derived from the cpu count and a single worker, the
 * queue can grow without bound while emitting no log lines at all, because nothing fails — jobs just
 * wait. A wait-duration histogram makes that visible; throughput and handler duration come from the same
 * listeners.
 *
 * Instrument naming, histogram boundaries and the attribute rule all follow `lib/authzed/metrics.ts` —
 * read the reasoning there, it applies verbatim. In short: Prometheus-style names with the unit spelled
 * out are the only spelling the Prometheus reader and the OTLP reader agree on, second-scale boundaries
 * are load-bearing because the SDK's defaults are a millisecond scale, and **every attribute is a
 * bounded, enumerable value — never an identifier.** `@forma/jobs` guarantees the second half: a job name
 * reaches the worker from Redis, and anything outside the registry arrives here as `unknown`.
 */

const meter = metrics.getMeter("forma.jobs");

/** Settled jobs by outcome. `job_name` is a closed set plus `unknown`; see `@forma/jobs` observability. */
const processedTotal = meter.createCounter("forma_jobs_processed_total", {
  description: "Background jobs that settled, by job name and outcome",
});

/**
 * Time spent inside the handler, in seconds.
 *
 * Boundaries run out to five minutes rather than the ten seconds the AuthZed histograms use: these
 * handlers make outbound calls the queue does not control — a webhook POST, an SMTP send, an AI
 * provider — so a healthy p99 here is seconds, not milliseconds, and an interesting tail is minutes.
 */
const durationSeconds = meter.createHistogram("forma_jobs_duration_seconds", {
  advice: {
    explicitBucketBoundaries: [0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300],
  },
  description: "Duration of a background job handler",
  unit: "s",
});

/**
 * Time a job waited past its intended run time, in seconds. This is the backlog signal.
 *
 * `@forma/jobs` measures it from `timestamp + delay`, so a recurring sweep queued 24h ahead contributes
 * its lateness rather than its interval. Boundaries reach ten minutes because a saturated queue is
 * interesting long before it is fatal, and the sub-second buckets keep the healthy case readable.
 */
const waitDurationSeconds = meter.createHistogram("forma_jobs_wait_duration_seconds", {
  advice: {
    explicitBucketBoundaries: [0.01, 0.05, 0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600],
  },
  description: "Time a background job waited past its intended run time before a worker picked it up",
  unit: "s",
});

/**
 * Enqueue attempts by outcome.
 *
 * `status="failed"` is the only trace a lost event leaves. `sendToPipeline` is contractually
 * never-throwing (a throw turned one survey submission into up to four response rows), so a Valkey blip
 * drops the response pipeline event and the request still returns 200 — no webhook, no follow-up email,
 * no workflow run, no billing event, and no error reaches anyone. Alert on a sustained non-zero rate
 * here for `job_name="response-pipeline.process"`.
 */
const enqueueTotal = meter.createCounter("forma_jobs_enqueue_total", {
  description: "Background job enqueue attempts, by job name and outcome",
});

/**
 * Retries are excluded from the wait histogram on purpose.
 *
 * A retried job keeps its original `timestamp`, and BullMQ's exponential backoff holds it for 5s then
 * 10s before the next attempt. Recording that as queue wait would report tens of seconds of backlog on
 * an idle queue every time one webhook endpoint is flaky, which is the opposite of the signal this is
 * for. Retry pressure is already visible as `forma_jobs_processed_total{status="failed"}`.
 */
const isFirstAttempt = (attemptsMade: number): boolean => attemptsMade <= 1;

export const jobsMetricsObserver: JobsObserver = {
  onEnqueue: ({ jobName, status }) => {
    enqueueTotal.add(1, { job_name: jobName, status });
  },
  onJobOutcome: ({ attemptsMade, durationMs, jobName, status, waitDurationMs }) => {
    processedTotal.add(1, { job_name: jobName, status });

    if (durationMs !== undefined) {
      durationSeconds.record(durationMs / 1000, { job_name: jobName, status });
    }

    if (waitDurationMs !== undefined && isFirstAttempt(attemptsMade)) {
      waitDurationSeconds.record(waitDurationMs / 1000, { job_name: jobName });
    }
  },
};

/**
 * Registered from `instrumentation-jobs.ts` at module scope rather than inside the worker bootstrap:
 * enqueues happen in the request path even when this process runs no worker
 * (`BULLMQ_EXTERNAL_WORKER_ENABLED`), and those are exactly the ones whose loss is invisible. With no
 * exporter configured `getMeter` returns a no-op meter, so this costs nothing on a default install.
 */
export const registerJobsMetrics = (): void => {
  setJobsObserver(jobsMetricsObserver);
};
