import { JOB_NAMES } from "@/src/constants";

/**
 * Observation seam for the queue that carries every survey response, webhook, follow-up email and
 * workflow run.
 *
 * The package deliberately holds no OpenTelemetry dependency: `apps/web` is the process that configures
 * the Prometheus and OTLP readers (`instrumentation-node.ts`), so it owns the instruments and registers
 * an observer here at boot (`apps/web/lib/jobs/metrics.ts`). This module only emits bounded facts.
 *
 * Registration goes through `globalThis` for the same reason the producer queue does: under `next dev`
 * a module can be evaluated more than once, and an observer registered against one copy would leave the
 * copy the request path enqueues through reporting nothing.
 *
 * **Every field is a bounded, enumerable value — never an identifier.** A job name reaches the worker
 * from Redis, so it is not inherently bounded: a schedule outliving the code that handled it feeds an
 * arbitrary string into the `completed` listener. `toBoundedJobName` collapses anything outside the
 * registry to `unknown`, which is what keeps a stale schedule from becoming a cardinality leak.
 */

export const UNKNOWN_JOB_NAME = "unknown";

const KNOWN_JOB_NAMES: ReadonlySet<string> = new Set(Object.values(JOB_NAMES));

/** Collapses any name outside the registry to a single `unknown` bucket. */
export const toBoundedJobName = (jobName: string | undefined): string =>
  jobName !== undefined && KNOWN_JOB_NAMES.has(jobName) ? jobName : UNKNOWN_JOB_NAME;

export type TJobEnqueueStatus = "enqueued" | "failed";
export type TJobOutcomeStatus = "completed" | "failed";

export interface TJobEnqueueObservation {
  jobName: string;
  status: TJobEnqueueStatus;
}

export interface TJobOutcomeObservation {
  /** Attempts BullMQ has recorded once the job settled: 1 on a first-attempt success. */
  attemptsMade: number;
  /** Time spent inside the handler, or undefined when BullMQ reported no processing window. */
  durationMs: number | undefined;
  jobName: string;
  status: TJobOutcomeStatus;
  /** Time the job spent waiting past its intended run time, or undefined when it cannot be derived. */
  waitDurationMs: number | undefined;
}

export interface JobsObserver {
  onEnqueue?: (observation: TJobEnqueueObservation) => void;
  onJobOutcome?: (observation: TJobOutcomeObservation) => void;
}

interface TGlobalJobsObserverState {
  formaJobsObserver: JobsObserver | undefined;
}

const globalForJobsObserver = globalThis as unknown as TGlobalJobsObserverState;

export const setJobsObserver = (observer: JobsObserver | undefined): void => {
  globalForJobsObserver.formaJobsObserver = observer;
};

/**
 * Observation can never change an outcome: a throwing exporter must not turn a delivered job into a
 * failed one, nor an accepted enqueue into a dropped response pipeline event. Swallowed silently rather
 * than logged, because the failure mode this guards against is an exporter failing on every call.
 */
const observeSafely = (observe: (observer: JobsObserver) => void): void => {
  const observer = globalForJobsObserver.formaJobsObserver;

  if (!observer) {
    return;
  }

  try {
    observe(observer);
  } catch {
    // Intentionally ignored — see above.
  }
};

export const recordJobEnqueue = (observation: TJobEnqueueObservation): void => {
  observeSafely((observer) => observer.onEnqueue?.(observation));
};

export const recordJobOutcome = (observation: TJobOutcomeObservation): void => {
  observeSafely((observer) => observer.onJobOutcome?.(observation));
};
