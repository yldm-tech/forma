import "server-only";
import { AUTHZED_CIRCUIT_COOLDOWN_MS } from "@/lib/authzed/constants";
import { AUTHZED_ERROR_CODES, AuthzedError } from "@/lib/authzed/errors";

/**
 * How a caller should treat an authorization operational failure it cannot answer.
 *
 * `transient` says the failure is about the engine's availability rather than about this request or this
 * deployment's configuration: SpiceDB was unreachable, saturated, slow, or the local circuit was open.
 * Those deserve a retryable response and a `warn`, because the caller retrying is the correct next step
 * and nothing here is a bug to page anyone about. Everything else — `authzed_invalid_request`,
 * `authzed_unauthenticated` (a misconfigured token), `authzed_unsupported`, a bare `authzed_internal` —
 * is a fault in this deployment that a retry cannot fix, so it stays a 500 and stays loud.
 *
 * The split is deliberately the `retryable` flag `AuthzedError` has carried since it was written, which
 * until now meant something only inside the retry loop.
 */
export type TAuthorizationOperationalDisposition = Readonly<{
  logLevel: "error" | "warn";
  retryAfterSeconds?: number;
  transient: boolean;
}>;

/**
 * The cooldown is the honest answer to "when is it worth asking again": until it elapses this process
 * will not even attempt a call, so anything sooner is a round trip that cannot succeed.
 */
const AUTHORIZATION_RETRY_AFTER_SECONDS = Math.ceil(AUTHZED_CIRCUIT_COOLDOWN_MS / 1000);

export const classifyAuthorizationOperationalError = (
  error: AuthzedError
): TAuthorizationOperationalDisposition =>
  error.retryable
    ? { logLevel: "warn", retryAfterSeconds: AUTHORIZATION_RETRY_AFTER_SECONDS, transient: true }
    : { logLevel: "error", transient: false };

export const normalizeAuthorizationOperationalError = (error: unknown, operation: string): AuthzedError => {
  if (error instanceof AuthzedError) {
    return new AuthzedError({
      attempts: error.attempts,
      code: error.code,
      grpcStatus: error.grpcStatus,
      operation,
      retryable: error.retryable,
    });
  }

  return new AuthzedError({
    attempts: 1,
    code: AUTHZED_ERROR_CODES.INTERNAL,
    operation,
    retryable: false,
  });
};
