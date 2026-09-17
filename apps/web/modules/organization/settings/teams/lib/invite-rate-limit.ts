import "server-only";
import { INVITE_RATE_LIMIT_PER_24_HOURS, IS_FORMA_CLOUD } from "@/lib/constants";
import { applyRateLimit } from "@/modules/core/rate-limit/helpers";
import { rateLimitConfigs } from "@/modules/core/rate-limit/rate-limit-configs";
import type { TRateLimitConfig } from "@/modules/core/rate-limit/types/rate-limit";

const CLOUD_BULK_INVITE_RATE_LIMIT_PER_24_HOURS = 500;

export const getInviteRateLimitConfig = async (): Promise<TRateLimitConfig> => {
  if (!IS_FORMA_CLOUD) {
    return {
      ...rateLimitConfigs.actions.inviteMember,
      allowedPerInterval: INVITE_RATE_LIMIT_PER_24_HOURS,
    };
  }

  return {
    ...rateLimitConfigs.actions.inviteMember,
    allowedPerInterval: CLOUD_BULK_INVITE_RATE_LIMIT_PER_24_HOURS,
  };
};

export const applyInviteRateLimit = async (organizationId: string, recipients = 1): Promise<void> => {
  await applyRateLimit(await getInviteRateLimitConfig(), organizationId, recipients);
};
