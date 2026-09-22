import { loggerMocks } from "./__mocks__/logger";
import { status } from "@grpc/grpc-js";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { type TAuthzedCircuitBreaker, createAuthzedCircuitBreaker } from "./circuit-breaker";
import {
  AUTHZED_CIRCUIT_COOLDOWN_MS,
  AUTHZED_CIRCUIT_FAILURE_THRESHOLD,
  AUTHZED_MAX_ATTEMPTS,
} from "./constants";
import { AUTHZED_ERROR_CODES, AuthzedError } from "./errors";
import { type TAuthzedRetryOptions, calculateAuthzedRetryDelayMs, executeAuthzedOperation } from "./retry";

describe("AuthZed retry policy", () => {
  // Every case gets its own breaker: the module singleton is process-wide by design, and sharing it
  // across tests would make one case's terminal failures decide another case's first call.
  let breaker: TAuthzedCircuitBreaker;

  const run = <T>(
    operation: string,
    request: () => Promise<T>,
    options: TAuthzedRetryOptions = {}
  ): Promise<T> => executeAuthzedOperation(operation, request, { breaker, ...options });

  beforeEach(() => {
    breaker = createAuthzedCircuitBreaker();
    loggerMocks.debug.mockReset();
    loggerMocks.warn.mockReset();
  });

  test("returns a first-attempt success without sleeping", async () => {
    const request = vi.fn().mockResolvedValue("success");
    const sleep = vi.fn();

    await expect(run("read_schema", request, { sleep })).resolves.toBe("success");
    expect(request).toHaveBeenCalledTimes(1);
    expect(sleep).not.toHaveBeenCalled();
    expect(loggerMocks.debug).not.toHaveBeenCalled();
    expect(loggerMocks.warn).not.toHaveBeenCalled();
  });

  test.each([
    [0, 0, 80],
    [0, 0.5, 100],
    [0, 1, 120],
    [1, 0, 160],
    [1, 0.5, 200],
    [1, 1, 240],
  ])("applies bounded jitter for retry %i at random value %f", (retryIndex, randomValue, expected) => {
    expect(calculateAuthzedRetryDelayMs(retryIndex, randomValue)).toBe(expected);
  });

  test("retries transient failures and succeeds on the third attempt", async () => {
    const request = vi
      .fn()
      .mockRejectedValueOnce({ code: status.UNAVAILABLE })
      .mockRejectedValueOnce({ code: status.ABORTED })
      .mockResolvedValue("success");
    const sleep = vi.fn().mockResolvedValue(undefined);
    const random = vi.fn().mockReturnValueOnce(0.5).mockReturnValueOnce(0.5);

    await expect(run("read_schema", request, { now: () => 10, random, sleep })).resolves.toBe("success");

    expect(request).toHaveBeenCalledTimes(3);
    expect(sleep).toHaveBeenNthCalledWith(1, 100);
    expect(sleep).toHaveBeenNthCalledWith(2, 200);
    expect(loggerMocks.debug).toHaveBeenCalledTimes(2);
    expect(loggerMocks.warn).not.toHaveBeenCalled();
  });

  test("uses the scheduled delay before succeeding on the second attempt", async () => {
    vi.useFakeTimers();
    const request = vi.fn().mockRejectedValueOnce({ code: status.ABORTED }).mockResolvedValue("success");

    try {
      const resultPromise = run("read_schema", request, { random: () => 0.5 });
      await vi.advanceTimersByTimeAsync(99);
      expect(request).toHaveBeenCalledTimes(1);

      await vi.advanceTimersByTimeAsync(1);
      await expect(resultPromise).resolves.toBe("success");
      expect(request).toHaveBeenCalledTimes(2);
    } finally {
      vi.useRealTimers();
    }
  });

  test("stops after three attempts and exposes the final stable classification", async () => {
    const request = vi.fn().mockRejectedValue({ code: status.DEADLINE_EXCEEDED });
    const sleep = vi.fn().mockResolvedValue(undefined);

    const result = await run("read_schema", request, {
      now: () => 10,
      random: () => 0.5,
      sleep,
    }).catch((error: unknown) => error);

    expect(result).toBeInstanceOf(AuthzedError);
    expect(result).toMatchObject({
      attempts: 3,
      code: AUTHZED_ERROR_CODES.TIMEOUT,
      grpcStatus: status.DEADLINE_EXCEEDED,
      retryable: true,
    });
    expect(request).toHaveBeenCalledTimes(3);
    expect(sleep).toHaveBeenCalledTimes(2);
    expect(loggerMocks.warn).toHaveBeenCalledTimes(1);
  });

  test("does not retry permanent errors", async () => {
    const request = vi.fn().mockRejectedValue({ code: status.UNAUTHENTICATED });
    const sleep = vi.fn();

    const result = await run("read_schema", request, { sleep }).catch((error: unknown) => error);

    expect(result).toMatchObject({
      attempts: 1,
      code: AUTHZED_ERROR_CODES.UNAUTHENTICATED,
      retryable: false,
    });
    expect(request).toHaveBeenCalledTimes(1);
    expect(sleep).not.toHaveBeenCalled();
  });

  test("logs only sanitized retry metadata", async () => {
    const token = "never-log-this-authzed-token";
    const request = vi.fn().mockRejectedValue({
      code: status.UNAUTHENTICATED,
      details: `Bearer ${token}`,
      metadata: { authorization: token },
    });

    await run("read_schema", request).catch(() => undefined);

    const serializedLogs = JSON.stringify([...loggerMocks.debug.mock.calls, ...loggerMocks.warn.mock.calls]);
    expect(serializedLogs).toContain(AUTHZED_ERROR_CODES.UNAUTHENTICATED);
    expect(serializedLogs).not.toContain(token);
    expect(serializedLogs).not.toContain("metadata");
    expect(serializedLogs).not.toContain("Bearer");
  });

  test("does not retry an overloaded server on the request path", async () => {
    const request = vi.fn().mockRejectedValue({ code: status.RESOURCE_EXHAUSTED });
    const sleep = vi.fn();

    const result = await run("check_permission", request, { sleep }).catch((error: unknown) => error);

    // RESOURCE_EXHAUSTED is SpiceDB saying it is already past capacity. Three Checks per check is a 3x
    // multiplier applied to the one failure mode that load caused.
    expect(request).toHaveBeenCalledTimes(1);
    expect(sleep).not.toHaveBeenCalled();
    expect(result).toMatchObject({
      attempts: 1,
      code: AUTHZED_ERROR_CODES.OVERLOADED,
      retryable: true,
    });
  });

  test("keeps retrying an overloaded server for bulk work", async () => {
    const request = vi.fn().mockRejectedValue({ code: status.RESOURCE_EXHAUSTED });
    const sleep = vi.fn().mockResolvedValue(undefined);

    const result = await run("write_relationships", request, {
      policy: "bulk",
      random: () => 0.5,
      sleep,
    }).catch((error: unknown) => error);

    expect(request).toHaveBeenCalledTimes(AUTHZED_MAX_ATTEMPTS);
    expect(sleep).toHaveBeenCalledTimes(AUTHZED_MAX_ATTEMPTS - 1);
    expect(result).toMatchObject({ code: AUTHZED_ERROR_CODES.OVERLOADED });
  });

  test("opens the circuit after repeated saturation failures and then answers without a round trip", async () => {
    const openBreaker = createAuthzedCircuitBreaker({ now: () => 0 });
    const request = vi.fn().mockRejectedValue({ code: status.UNAVAILABLE });
    const sleep = vi.fn().mockResolvedValue(undefined);
    const options = { breaker: openBreaker, random: () => 0.5, sleep };

    for (let failure = 0; failure < AUTHZED_CIRCUIT_FAILURE_THRESHOLD; failure += 1) {
      await executeAuthzedOperation("check_permission", request, options).catch(() => undefined);
    }

    const callsBeforeOpen = request.mock.calls.length;
    const shortCircuited = await executeAuthzedOperation("check_permission", request, options).catch(
      (error: unknown) => error
    );

    expect(callsBeforeOpen).toBe(AUTHZED_CIRCUIT_FAILURE_THRESHOLD * AUTHZED_MAX_ATTEMPTS);
    expect(request).toHaveBeenCalledTimes(callsBeforeOpen);
    // Still an operational failure, and still fails closed as an error rather than as a denial — the
    // circuit changes what it costs to find out, not what the answer is.
    expect(shortCircuited).toBeInstanceOf(AuthzedError);
    expect(shortCircuited).toMatchObject({
      attempts: 0,
      code: AUTHZED_ERROR_CODES.UNAVAILABLE,
      retryable: true,
    });
  });

  test("admits one single-attempt probe per cooldown and closes the circuit when it succeeds", async () => {
    let clock = 0;
    const probeBreaker = createAuthzedCircuitBreaker({ now: () => clock });
    const request = vi.fn().mockRejectedValue({ code: status.UNAVAILABLE });
    const sleep = vi.fn().mockResolvedValue(undefined);
    const options = { breaker: probeBreaker, random: () => 0.5, sleep };

    for (let failure = 0; failure < AUTHZED_CIRCUIT_FAILURE_THRESHOLD; failure += 1) {
      await executeAuthzedOperation("check_permission", request, options).catch(() => undefined);
    }

    clock += AUTHZED_CIRCUIT_COOLDOWN_MS;
    request.mockClear();
    await executeAuthzedOperation("check_permission", request, options).catch(() => undefined);

    // A probe against a still-sick server costs one call, not three plus two backoffs.
    expect(request).toHaveBeenCalledTimes(1);

    await expect(executeAuthzedOperation("check_permission", request, options)).rejects.toMatchObject({
      attempts: 0,
    });

    clock += AUTHZED_CIRCUIT_COOLDOWN_MS;
    request.mockClear();
    request.mockResolvedValue("recovered");

    await expect(executeAuthzedOperation("check_permission", request, options)).resolves.toBe("recovered");
    await expect(executeAuthzedOperation("check_permission", request, options)).resolves.toBe("recovered");
    expect(request).toHaveBeenCalledTimes(2);
  });
});
