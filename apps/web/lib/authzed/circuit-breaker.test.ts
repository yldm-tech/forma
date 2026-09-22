import { describe, expect, test } from "vitest";
import { createAuthzedCircuitBreaker, isAuthzedSaturationCode } from "./circuit-breaker";
import { AUTHZED_CIRCUIT_COOLDOWN_MS, AUTHZED_CIRCUIT_FAILURE_THRESHOLD } from "./constants";
import { AUTHZED_ERROR_CODES, type TAuthzedErrorCode } from "./errors";

const failTimes = (
  breaker: ReturnType<typeof createAuthzedCircuitBreaker>,
  count: number,
  code: TAuthzedErrorCode = AUTHZED_ERROR_CODES.UNAVAILABLE
): void => {
  for (let failure = 0; failure < count; failure += 1) {
    breaker.admit();
    breaker.recordFailure(code);
  }
};

describe("AuthZed request circuit breaker", () => {
  test("stays closed below the failure threshold", () => {
    const breaker = createAuthzedCircuitBreaker({ now: () => 0 });

    failTimes(breaker, AUTHZED_CIRCUIT_FAILURE_THRESHOLD - 1);

    expect(breaker.state()).toBe("closed");
    expect(breaker.admit()).toEqual({ admitted: true, probe: false });
  });

  test("opens on consecutive saturation failures and refuses further calls during the cooldown", () => {
    const breaker = createAuthzedCircuitBreaker({ now: () => 0 });

    failTimes(breaker, AUTHZED_CIRCUIT_FAILURE_THRESHOLD, AUTHZED_ERROR_CODES.OVERLOADED);

    expect(breaker.state()).toBe("open");
    // The refusal replays the code it opened on, so the caller cannot tell a short circuit from the
    // failure it stands in for.
    expect(breaker.admit()).toEqual({ admitted: false, code: AUTHZED_ERROR_CODES.OVERLOADED });
  });

  test("a success anywhere in the streak keeps the circuit closed", () => {
    const breaker = createAuthzedCircuitBreaker({ now: () => 0 });

    failTimes(breaker, AUTHZED_CIRCUIT_FAILURE_THRESHOLD - 1);
    breaker.recordSuccess();
    failTimes(breaker, AUTHZED_CIRCUIT_FAILURE_THRESHOLD - 1);

    expect(breaker.state()).toBe("closed");
  });

  test.each([
    AUTHZED_ERROR_CODES.INVALID_REQUEST,
    AUTHZED_ERROR_CODES.NOT_FOUND,
    AUTHZED_ERROR_CODES.UNAUTHENTICATED,
  ])("never opens on %s, which is a healthy server answering or a fault here", (code) => {
    const breaker = createAuthzedCircuitBreaker({ now: () => 0 });

    failTimes(breaker, AUTHZED_CIRCUIT_FAILURE_THRESHOLD * 2, code);

    expect(breaker.state()).toBe("closed");
    expect(isAuthzedSaturationCode(code)).toBe(false);
  });

  test("admits exactly one probe per cooldown", () => {
    let clock = 0;
    const breaker = createAuthzedCircuitBreaker({ now: () => clock });

    failTimes(breaker, AUTHZED_CIRCUIT_FAILURE_THRESHOLD);
    clock += AUTHZED_CIRCUIT_COOLDOWN_MS - 1;

    expect(breaker.admit()).toEqual({ admitted: false, code: AUTHZED_ERROR_CODES.UNAVAILABLE });

    clock += 1;

    expect(breaker.admit()).toEqual({ admitted: true, probe: true });
    expect(breaker.state()).toBe("half_open");
    // Concurrent callers wait for the probe's answer instead of becoming a second, third and fourth one.
    expect(breaker.admit()).toEqual({ admitted: false, code: AUTHZED_ERROR_CODES.UNAVAILABLE });
  });

  test("a failed probe reopens for a full cooldown rather than rebuilding the streak", () => {
    let clock = 0;
    const breaker = createAuthzedCircuitBreaker({ now: () => clock });

    failTimes(breaker, AUTHZED_CIRCUIT_FAILURE_THRESHOLD);
    clock += AUTHZED_CIRCUIT_COOLDOWN_MS;
    breaker.admit();
    breaker.recordFailure(AUTHZED_ERROR_CODES.TIMEOUT);

    expect(breaker.state()).toBe("open");
    expect(breaker.admit()).toEqual({ admitted: false, code: AUTHZED_ERROR_CODES.TIMEOUT });

    clock += AUTHZED_CIRCUIT_COOLDOWN_MS;

    expect(breaker.admit()).toEqual({ admitted: true, probe: true });
  });

  test("a successful probe closes the circuit", () => {
    let clock = 0;
    const breaker = createAuthzedCircuitBreaker({ now: () => clock });

    failTimes(breaker, AUTHZED_CIRCUIT_FAILURE_THRESHOLD);
    clock += AUTHZED_CIRCUIT_COOLDOWN_MS;
    breaker.admit();
    breaker.recordSuccess();

    expect(breaker.state()).toBe("closed");
    expect(breaker.admit()).toEqual({ admitted: true, probe: false });
  });
});
