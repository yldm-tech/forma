import "server-only";
import { prisma } from "@forma/database";
import { logger } from "@forma/logger";
import { TIntegrationType } from "@forma/types/integration";
import {
  TIntegrationDeliveryOutcome,
  resolveIntegrationHealthTransition,
} from "@/modules/integrations/lib/delivery-health";

export type TIntegrationDeliveryTarget = {
  id: string;
  workspaceId: string;
  type: TIntegrationType;
  /**
   * The row's current counter. Omit it only when the caller has not loaded the column: the success path
   * then falls back to a conditional UPDATE that matches no row in steady state, which still costs a round
   * trip the known-zero case skips entirely.
   */
  consecutiveFailures?: number | null;
};

/**
 * Record the outcome of one delivery against the integration's health columns.
 *
 * Writes on a state transition only. A failure increments the counter and stamps the error; a success
 * clears them, and only when there is something to clear — so a workspace delivering normally issues no
 * statement at all for this, which is why the row carries no `lastSyncAt` to contend on.
 *
 * Never throws. It runs inside the response pipeline's own failure containment, where losing the health
 * update matters far less than losing the response, and a row deleted mid-flight is an ordinary race
 * rather than an error: both writes are `updateMany`, so they touch zero rows instead of raising.
 */
export const recordIntegrationResult = async (
  integration: TIntegrationDeliveryTarget,
  outcome: TIntegrationDeliveryOutcome
): Promise<void> => {
  const transition = resolveIntegrationHealthTransition(integration.consecutiveFailures, outcome, new Date());

  if (transition.kind === "none") return;

  const { id, workspaceId } = integration;

  try {
    if (transition.kind === "failure") {
      // `increment` rather than a read-then-write: two destinations of the same type cannot race here
      // (`@@unique([type, workspaceId])`), but two responses in the same workspace can.
      await prisma.integration.updateMany({
        where: { id, workspaceId },
        data: {
          consecutiveFailures: { increment: 1 },
          lastErrorAt: transition.lastErrorAt,
          lastErrorMessage: transition.lastErrorMessage,
        },
      });
      return;
    }

    // `consecutiveFailures: { gt: 0 }` keeps the recovery write conditional in the database as well, so a
    // caller that could not tell us the current value still never rewrites an already-clean row.
    await prisma.integration.updateMany({
      where: { id, workspaceId, consecutiveFailures: { gt: 0 } },
      data: { consecutiveFailures: 0, lastErrorAt: null, lastErrorMessage: null },
    });
  } catch (error) {
    logger.error(
      { err: error, integrationId: id, integrationType: integration.type },
      "Failed to record integration delivery health"
    );
  }
};
