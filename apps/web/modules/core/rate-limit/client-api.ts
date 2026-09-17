import "server-only";
import { getRateLimitErrorResponse } from "@/lib/api/client-rate-limit";
import { GATEWAY_RATE_LIMITING } from "@/lib/constants";
import { isRouteRateLimitedByEnvoy } from "@/modules/core/rate-limit/envoy-rate-limit-coverage";
import { applyIPRateLimit } from "@/modules/core/rate-limit/helpers";
import { rateLimitConfigs } from "@/modules/core/rate-limit/rate-limit-configs";

/**
 * The in-app rate limit for an unauthenticated client API route.
 *
 * Response and display submission is public and unauthenticated, so something has to bound it. The gateway policy set covers those paths, and `withV1ApiWrapper` has always skipped its own limiter wherever that policy applies — correct behind a gateway, and nothing at all without one. The v2 client handlers never entered that wrapper, so they had no limiter either way.
 *
 * Both halves are fixed by asking whether a gateway is actually declared: `GATEWAY_RATE_LIMITING` defaults off, matching the compose stack in `docker/`, which runs none.
 *
 * Returns a response when the caller is over the limit, `null` when it may proceed.
 */
export const applyClientApiRateLimit = async (request: Request): Promise<Response | null> => {
  const { pathname } = new URL(request.url);

  if (
    GATEWAY_RATE_LIMITING &&
    isRouteRateLimitedByEnvoy({ pathname, method: request.method, authType: "none" })
  ) {
    return null;
  }

  try {
    await applyIPRateLimit(rateLimitConfigs.api.client);
    return null;
  } catch (error) {
    return getRateLimitErrorResponse({ request, error, cors: true });
  }
};
