import { describe, expect, test } from "vitest";
import { AUTHZED_ERROR_CODES, AuthzedError } from "@/lib/authzed/errors";
import {
  classifyAuthorizationOperationalError,
  normalizeAuthorizationOperationalError,
} from "./operational-error";

describe("authorization operational error normalization", () => {
  test("preserves stable AuthZed fields while replacing the operation", () => {
    const source = new AuthzedError({
      attempts: 3,
      code: AUTHZED_ERROR_CODES.UNAVAILABLE,
      grpcStatus: 14,
      operation: "lookup_resources",
      retryable: true,
    });

    expect(normalizeAuthorizationOperationalError(source, "authorization")).toMatchObject({
      attempts: 3,
      code: AUTHZED_ERROR_CODES.UNAVAILABLE,
      grpcStatus: 14,
      operation: "authorization",
      retryable: true,
    });
  });

  test("sanitizes unexpected failures as non-retryable internal errors", () => {
    const normalized = normalizeAuthorizationOperationalError(
      new Error("private raw message"),
      "authorization"
    );

    expect(normalized).toMatchObject({
      attempts: 1,
      code: AUTHZED_ERROR_CODES.INTERNAL,
      operation: "authorization",
      retryable: false,
    });
    expect(normalized.message).not.toContain("private raw message");
    expect(normalized.stack ?? "").not.toContain("private raw message");
  });

  test.each([AUTHZED_ERROR_CODES.OVERLOADED, AUTHZED_ERROR_CODES.TIMEOUT, AUTHZED_ERROR_CODES.UNAVAILABLE])(
    "classifies %s as transient, with a retry hint the caller can act on",
    (code) => {
      const disposition = classifyAuthorizationOperationalError(
        new AuthzedError({ attempts: 3, code, operation: "authorization", retryable: true })
      );

      expect(disposition).toEqual({ logLevel: "warn", retryAfterSeconds: 5, transient: true });
    }
  );

  test.each([
    AUTHZED_ERROR_CODES.INTERNAL,
    AUTHZED_ERROR_CODES.INVALID_REQUEST,
    AUTHZED_ERROR_CODES.UNAUTHENTICATED,
    AUTHZED_ERROR_CODES.UNSUPPORTED,
  ])("keeps %s loud, because a retry cannot fix a fault in this deployment", (code) => {
    const disposition = classifyAuthorizationOperationalError(
      new AuthzedError({ attempts: 1, code, operation: "authorization", retryable: false })
    );

    expect(disposition).toEqual({ logLevel: "error", transient: false });
    expect(disposition.retryAfterSeconds).toBeUndefined();
  });

  test("classifies a short-circuited check as transient, like the failure it stands in for", () => {
    // The open circuit throws `attempts: 0` with the code it opened on; the caller must not be able to
    // tell that apart from an exhausted retry loop.
    const disposition = classifyAuthorizationOperationalError(
      new AuthzedError({
        attempts: 0,
        code: AUTHZED_ERROR_CODES.UNAVAILABLE,
        operation: "authorization",
        retryable: true,
      })
    );

    expect(disposition.transient).toBe(true);
  });
});
