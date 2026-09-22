import "server-only";
import { AUTHZED_CIRCUIT_COOLDOWN_MS, AUTHZED_CIRCUIT_FAILURE_THRESHOLD } from "./constants";
import { AUTHZED_ERROR_CODES, type TAuthzedErrorCode } from "./errors";
import { recordAuthzedRequestCircuitState } from "./metrics";

/**
 * Process-local circuit breaker for the request path.
 *
 * It exists because the retry policy amplifies exactly the failure it reports: when SpiceDB is
 * saturated, every check sends up to three Checks instead of one, and a slow-not-down server makes each
 * of them burn the full channel deadline. The breaker removes the multiplier for the whole saturation
 * class, and removes the deadline wait with it.
 *
 * **It never turns an outage into a denial.** A short-circuited call throws the same typed
 * `AuthzedError` the exhausted retry loop would have thrown, so `authorizationCoordinator` still records
 * `operational_error` and still fails closed as an error. Only latency and load change. That is also why
 * it counts *terminal* saturation failures only — an open circuit must be evidence about SpiceDB, not
 * about one unlucky call, and a fault in this process (`authzed_invalid_request`,
 * `authzed_unauthenticated`) is not something a cooldown can fix.
 */
export type TAuthzedCircuitState = "closed" | "half_open" | "open";

export type TAuthzedCircuitAdmission =
  Readonly<{ admitted: false; code: TAuthzedErrorCode }> | Readonly<{ admitted: true; probe: boolean }>;

export type TAuthzedCircuitBreaker = Readonly<{
  admit: () => TAuthzedCircuitAdmission;
  recordFailure: (code: TAuthzedErrorCode) => void;
  recordSuccess: () => void;
  state: () => TAuthzedCircuitState;
}>;

/**
 * The failure classes that mean SpiceDB is saturated or unreachable.
 *
 * The same three the runbook names for a `pgxpool_empty_acquire` burst. Everything else either came
 * from a healthy server answering (a denial, a not-found, an invalid argument) or is a misconfiguration
 * of this deployment, and neither says anything about capacity.
 */
const AUTHZED_SATURATION_CODES: ReadonlySet<TAuthzedErrorCode> = new Set<TAuthzedErrorCode>([
  AUTHZED_ERROR_CODES.OVERLOADED,
  AUTHZED_ERROR_CODES.TIMEOUT,
  AUTHZED_ERROR_CODES.UNAVAILABLE,
]);

export const isAuthzedSaturationCode = (code: TAuthzedErrorCode): boolean =>
  AUTHZED_SATURATION_CODES.has(code);

type TAuthzedCircuitDependencies = Readonly<{
  now?: () => number;
}>;

export const createAuthzedCircuitBreaker = (
  dependencies: TAuthzedCircuitDependencies = {}
): TAuthzedCircuitBreaker => {
  const now = dependencies.now ?? (() => Date.now());

  let state: TAuthzedCircuitState = "closed";
  let consecutiveFailures = 0;
  let openedAt = 0;
  let probeInFlight = false;
  // The code the circuit opened on, replayed to every caller it refuses so the short-circuit is
  // indistinguishable from the failure it stands in for.
  let lastFailureCode: TAuthzedErrorCode = AUTHZED_ERROR_CODES.UNAVAILABLE;

  const transitionTo = (next: TAuthzedCircuitState): void => {
    if (next === state) {
      return;
    }

    state = next;
    recordAuthzedRequestCircuitState(next);
  };

  const close = (): void => {
    consecutiveFailures = 0;
    probeInFlight = false;
    transitionTo("closed");
  };

  const open = (): void => {
    consecutiveFailures = 0;
    openedAt = now();
    probeInFlight = false;
    transitionTo("open");
  };

  return Object.freeze<TAuthzedCircuitBreaker>({
    admit: () => {
      if (state === "closed") {
        return { admitted: true, probe: false };
      }

      if (state === "open" && now() - openedAt < AUTHZED_CIRCUIT_COOLDOWN_MS) {
        return { admitted: false, code: lastFailureCode };
      }

      if (probeInFlight) {
        return { admitted: false, code: lastFailureCode };
      }

      probeInFlight = true;
      transitionTo("half_open");
      return { admitted: true, probe: true };
    },
    recordFailure: (code) => {
      if (!isAuthzedSaturationCode(code)) {
        // SpiceDB answered. Whatever is wrong is not capacity, so the streak is broken and an open
        // circuit has nothing left to protect.
        close();
        return;
      }

      lastFailureCode = code;

      // A failed probe reopens immediately: the cooldown just proved insufficient, and counting it
      // toward the threshold would admit a probe on every call until the count built back up.
      if (probeInFlight || state === "half_open") {
        open();
        return;
      }

      consecutiveFailures += 1;
      if (consecutiveFailures >= AUTHZED_CIRCUIT_FAILURE_THRESHOLD) {
        open();
      }
    },
    recordSuccess: () => {
      close();
    },
    state: () => state,
  });
};

/** The breaker the request-path retry policy uses. One per process, like the client it guards. */
export const authzedRequestCircuitBreaker = createAuthzedCircuitBreaker();
