import "server-only";
import { randomInt } from "node:crypto";
import { performance } from "node:perf_hooks";
import { logger } from "@forma/logger";
import { type TAuthzedCircuitBreaker, authzedRequestCircuitBreaker } from "./circuit-breaker";
import { AUTHZED_MAX_ATTEMPTS, AUTHZED_RETRY_BASE_DELAYS_MS, AUTHZED_RETRY_JITTER_RATIO } from "./constants";
import { AUTHZED_ERROR_CODES, AuthzedError, type TAuthzedErrorCode, mapAuthzedError } from "./errors";
import {
  recordAuthzedRequestFailure,
  recordAuthzedRequestRetry,
  recordAuthzedRequestShortCircuit,
} from "./metrics";

/**
 * Which process this channel serves, and therefore what a retry costs.
 *
 * `request` is a user waiting: the work is one cheap call, and load added to a sick SpiceDB is paid by
 * every other request in flight. `bulk` is a command-line backfill or sweep: it *is* the load, nobody is
 * waiting on it, and abandoning a unit costs more than the retry does.
 */
export type TAuthzedRetryPolicy = "bulk" | "request";

type TAuthzedRetryDependencies = Readonly<{
  now: () => number;
  random: () => number;
  sleep: (delayMs: number) => Promise<void>;
}>;

export type TAuthzedRetryOptions = Partial<TAuthzedRetryDependencies> &
  Readonly<{
    breaker?: TAuthzedCircuitBreaker;
    policy?: TAuthzedRetryPolicy;
  }>;

export type TAuthzedOperationRunner = <T>(
  operation: string,
  request: () => Promise<T>,
  dependencyOverrides?: Partial<TAuthzedRetryDependencies>
) => Promise<T>;

const AUTHZED_RETRY_RANDOM_SCALE = 1_000_000;

const defaultDependencies: TAuthzedRetryDependencies = {
  now: () => performance.now(),
  random: () => randomInt(AUTHZED_RETRY_RANDOM_SCALE + 1) / AUTHZED_RETRY_RANDOM_SCALE,
  sleep: (delayMs) => new Promise((resolve) => setTimeout(resolve, delayMs)),
};

export const calculateAuthzedRetryDelayMs = (retryIndex: number, randomValue: number): number => {
  const baseDelayMs = AUTHZED_RETRY_BASE_DELAYS_MS[retryIndex];

  if (baseDelayMs === undefined) {
    throw new RangeError(`Unsupported AuthZed retry index: ${retryIndex}`);
  }

  const boundedRandomValue = Math.min(1, Math.max(0, randomValue));
  const jitterMultiplier =
    1 - AUTHZED_RETRY_JITTER_RATIO + boundedRandomValue * AUTHZED_RETRY_JITTER_RATIO * 2;

  return Math.round(baseDelayMs * jitterMultiplier);
};

/**
 * Whether a classified failure is worth another attempt under `policy`.
 *
 * `authzed_overloaded` is the one retryable class where retrying is counterproductive: it is SpiceDB
 * answering that it is already past capacity, so the request path answering with two more Checks triples
 * the load on the exact failure that load caused. Under `bulk` the trade-off inverts — the backoff is the
 * point, and giving up strands a unit of a backfill that has to converge.
 */
const shouldRetryUnderPolicy = (
  code: TAuthzedErrorCode,
  retryable: boolean,
  policy: TAuthzedRetryPolicy
): boolean => retryable && (policy === "bulk" || code !== AUTHZED_ERROR_CODES.OVERLOADED);

export const executeAuthzedOperation = async <T>(
  operation: string,
  request: () => Promise<T>,
  options: TAuthzedRetryOptions = {}
): Promise<T> => {
  const { breaker: breakerOverride, policy: policyOverride, ...dependencyOverrides } = options;
  const dependencies = { ...defaultDependencies, ...dependencyOverrides };
  const policy = policyOverride ?? "request";
  // Bulk work is deliberately unguarded: a backfill that gives up on a blip leaves the projection
  // drifted, and there is no user whose latency the circuit would be protecting.
  const breaker = policy === "bulk" ? undefined : (breakerOverride ?? authzedRequestCircuitBreaker);
  const startedAt = dependencies.now();
  const admission = breaker?.admit();

  if (admission && !admission.admitted) {
    logger.warn(
      {
        attemptCount: 0,
        component: "authzed",
        errorCode: admission.code,
        operation,
      },
      "AuthZed request short-circuited by an open circuit"
    );
    recordAuthzedRequestShortCircuit({ code: admission.code, operation });
    throw new AuthzedError({
      attempts: 0,
      code: admission.code,
      operation,
      retryable: true,
    });
  }

  // A half-open probe asks whether SpiceDB is back, and nothing more: if it is still sick, the cheapest
  // possible answer is one call rather than three plus two backoffs.
  const maxAttempts = admission?.probe ? 1 : AUTHZED_MAX_ATTEMPTS;

  for (let attempt = 1; attempt <= maxAttempts; attempt += 1) {
    try {
      const result = await request();
      breaker?.recordSuccess();
      return result;
    } catch (error) {
      const authzedError = mapAuthzedError(error, operation, attempt);
      const durationMs = Math.max(0, Math.round(dependencies.now() - startedAt));
      const shouldRetry =
        shouldRetryUnderPolicy(authzedError.code, authzedError.retryable, policy) && attempt < maxAttempts;

      if (!shouldRetry) {
        logger.warn(
          {
            attemptCount: attempt,
            component: "authzed",
            durationMs,
            errorCode: authzedError.code,
            grpcStatus: authzedError.grpcStatus,
            operation,
            retryable: authzedError.retryable,
          },
          "AuthZed request failed"
        );
        recordAuthzedRequestFailure({
          code: authzedError.code,
          operation,
          retryable: authzedError.retryable,
        });
        breaker?.recordFailure(authzedError.code);
        throw authzedError;
      }

      const retryDelayMs = calculateAuthzedRetryDelayMs(attempt - 1, dependencies.random());
      logger.debug(
        {
          attemptCount: attempt,
          component: "authzed",
          durationMs,
          errorCode: authzedError.code,
          grpcStatus: authzedError.grpcStatus,
          operation,
          retryable: authzedError.retryable,
          retryDelayMs,
        },
        "AuthZed request retry scheduled"
      );
      // Retries that still succeed never reach the failure counter, so without this a degraded SpiceDB
      // is invisible until it starts dropping writes outright.
      recordAuthzedRequestRetry({ code: authzedError.code, operation });
      await dependencies.sleep(retryDelayMs);
    }
  }

  throw new Error("AuthZed retry loop exited unexpectedly");
};

/**
 * Bind a policy to every call on one channel.
 *
 * The policy is a property of the process, exactly as the channel deadline is, and `createAuthzedClient`
 * is the only place that knows which one this process took. Binding it there keeps this module free of
 * any dependency on the client it serves.
 */
export const createAuthzedOperationRunner =
  (policy: TAuthzedRetryPolicy): TAuthzedOperationRunner =>
  (operation, request, dependencyOverrides = {}) =>
    executeAuthzedOperation(operation, request, { ...dependencyOverrides, policy });
