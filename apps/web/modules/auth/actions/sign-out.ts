"use server";

import { z } from "zod";
import { logger } from "@forma/logger";
import { ZId } from "@forma/types/common";
import { getMembershipByUserIdOrganizationId } from "@/lib/membership/service";
import { authenticatedActionClient } from "@/lib/utils/action-client";
import { logSignOut } from "@/modules/auth/lib/utils";

const ZLogSignOutAction = z.object({
  reason: z
    .enum([
      "user_initiated",
      "account_deletion",
      "email_change",
      "session_timeout",
      "forced_logout",
      "password_reset",
    ])
    .optional(),
  redirectUrl: z.string().optional(),
  organizationId: ZId.optional(),
});

/**
 * Records the audit entry for a sign-out.
 *
 * The actor comes from the session, never from the caller. This is a Server Action, so it is an HTTP
 * endpoint anyone able to load the app can POST to; with a caller-supplied id a forged
 * `userSignedOut` row is indistinguishable from a real one and the audit log stops being evidence.
 * `organizationId` is only stamped once the session user is shown to belong to it.
 */
export const logSignOutAction = authenticatedActionClient
  .inputSchema(ZLogSignOutAction)
  .action(async ({ ctx, parsedInput }) => {
    let organizationId: string | undefined;
    if (parsedInput.organizationId) {
      const membership = await getMembershipByUserIdOrganizationId(ctx.user.id, parsedInput.organizationId);
      organizationId = membership ? parsedInput.organizationId : undefined;
    }

    try {
      logSignOut(ctx.user.id, ctx.user.email, {
        reason: parsedInput.reason,
        redirectUrl: parsedInput.redirectUrl,
        organizationId,
      });
    } catch (error) {
      logger.error(
        {
          userId: ctx.user.id,
          error: error instanceof Error ? error.message : String(error),
        },
        "Failed to log sign out event"
      );
      throw error;
    }
  });
