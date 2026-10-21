import "server-only";
import type { JobHandler, TRetentionSweepJobData } from "@forma/jobs";
import { logger } from "@forma/logger";
import { runRetentionSweep } from "./retention-sweep";

/**
 * Applies the configured retention windows to terminal workflow runs and unconverted displays.
 *
 * The completion line is the point of this release: while the sweep is in dry-run mode it is the only
 * place an operator can see how many rows a window would remove before one does.
 */
export const processRetentionSweepJob: JobHandler<TRetentionSweepJobData> = async (data, context) => {
  const logContext = {
    attempt: context.attempt,
    jobId: context.jobId,
    jobName: context.jobName,
    maxAttempts: context.maxAttempts,
    queueName: context.queueName,
    scope: data.scope,
  };

  logger.info(logContext, "Retention sweep job started");

  const result = await runRetentionSweep();

  logger.info(
    {
      ...logContext,
      displays: result.displays,
      dryRun: result.dryRun,
      invalidWindows: result.invalidWindows,
      organizations: result.organizations,
      workflowRuns: result.workflowRuns,
    },
    result.dryRun
      ? "Retention sweep job completed (dry run, nothing deleted)"
      : "Retention sweep job completed"
  );
};
