import type { TWorkflowRunLog } from "../api";

// How a run's detail is shown, forma2's modules/workflows/lib/run-display.ts.

/** The characters one run JSON blob (trigger payload, run data, a step's input or output) renders at most; past it the text is cut with a notice, so one huge payload cannot blow up the page. */
export const MAX_RUN_JSON_CHARS = 50_000;

/** A step's duration as a compact label, or null while it has not started and finished or the timestamps give no sane elapsed time. */
export const formatStepDuration = (startedAt: string | null, finishedAt: string | null): string | null => {
  if (!startedAt || !finishedAt) return null;
  const ms = new Date(finishedAt).getTime() - new Date(startedAt).getTime();
  if (!Number.isFinite(ms) || ms < 0) return null;
  return ms < 1000 ? `${ms}ms` : `${(ms / 1000).toFixed(1)}s`;
};

export const hasKeys = (value: Record<string, unknown>): boolean => Object.keys(value).length > 0;

/** A run JSON value pretty-printed, cut at maxChars with a notice of the full length. */
export const stringifyRunJson = (value: unknown, maxChars: number = MAX_RUN_JSON_CHARS): string => {
  const json = JSON.stringify(value, null, 2) ?? String(value);
  if (json.length <= maxChars) return json;
  return `${json.slice(0, maxChars)}\n\n… truncated — ${json.length} characters total`;
};

/** Whether a step failed and the run went on past it: the step was set to continue on failure, which its run log input records. */
export const isContinuedFailure = (log: Pick<TWorkflowRunLog, "status" | "input">): boolean =>
  log.status === "failed" && log.input.continueOnFailure === true;
