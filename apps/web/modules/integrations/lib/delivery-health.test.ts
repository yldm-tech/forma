import { describe, expect, test } from "vitest";
import { Result } from "@forma/types/error-handlers";
import {
  INTEGRATION_ERROR_MESSAGE_LIMIT,
  INTEGRATION_RECONNECT_FAILURE_THRESHOLD,
  resolveIntegrationHealthState,
  resolveIntegrationHealthTransition,
  toIntegrationDeliveryOutcome,
  toIntegrationErrorMessage,
} from "./delivery-health";

const NOW = new Date("2026-09-22T10:00:00.000Z");

describe("toIntegrationErrorMessage", () => {
  test("keeps the provider's own wording", () => {
    expect(toIntegrationErrorMessage(new Error("Notion API error creating page: 404 Not Found"))).toBe(
      "Notion API error creating page: 404 Not Found"
    );
  });

  test("redacts a credential echoed back in a JSON error body", () => {
    const message = toIntegrationErrorMessage(
      new Error('Airtable rejected the write: {"access_token":"patABC.123secret","code":"INVALID"}')
    );
    expect(message).not.toContain("patABC.123secret");
    expect(message).toContain('"access_token":"[redacted]"');
    expect(message).toContain('"code":"INVALID"');
  });

  test("redacts a credential carried in a quoted request URL", () => {
    const message = toIntegrationErrorMessage(
      new Error("GET https://example.com/v1/rows?refresh_token=1%2FabcSECRET&pageSize=10 failed")
    );
    expect(message).not.toContain("1%2FabcSECRET");
    expect(message).toContain("refresh_token=[redacted]");
    expect(message).toContain("pageSize=10");
  });

  test("redacts an Authorization header value", () => {
    const message = toIntegrationErrorMessage(new Error("headers: Authorization: Bearer ya29.a0AfB_TOKEN"));
    expect(message).not.toContain("ya29.a0AfB_TOKEN");
    expect(message).toContain("Bearer [redacted]");
  });

  test("collapses newlines and control characters into one storable line", () => {
    expect(toIntegrationErrorMessage(new Error("row 1 failed\n\tbecause\u0000 of a limit"))).toBe(
      "row 1 failed because of a limit"
    );
  });

  test("truncates a provider that dumps a whole response body", () => {
    const message = toIntegrationErrorMessage(new Error("x".repeat(5_000)));
    expect(message).toHaveLength(INTEGRATION_ERROR_MESSAGE_LIMIT + 3);
    expect(message.endsWith("...")).toBe(true);
  });

  test("falls back rather than storing an empty string for a non-Error throw", () => {
    expect(toIntegrationErrorMessage({ status: 500 })).toBe("Unknown integration delivery error");
    expect(toIntegrationErrorMessage(new Error("   "))).toBe("Unknown integration delivery error");
  });

  test("reads a thrown string", () => {
    expect(toIntegrationErrorMessage("sheet not found")).toBe("sheet not found");
  });
});

describe("toIntegrationDeliveryOutcome", () => {
  test("treats a rejection and an ok:false result as the same failure", () => {
    const rejected: PromiseSettledResult<Result<void, Error>> = {
      status: "rejected",
      reason: new Error("boom"),
    };
    const failed: PromiseSettledResult<Result<void, Error>> = {
      status: "fulfilled",
      value: { ok: false, error: new Error("boom") },
    };

    expect(toIntegrationDeliveryOutcome(rejected)).toEqual({ ok: false, error: new Error("boom") });
    expect(toIntegrationDeliveryOutcome(failed)).toEqual({ ok: false, error: new Error("boom") });
  });

  test("reports a fulfilled ok result as a success", () => {
    expect(
      toIntegrationDeliveryOutcome({ status: "fulfilled", value: { ok: true, data: undefined } })
    ).toEqual({ ok: true });
  });
});

describe("resolveIntegrationHealthTransition", () => {
  test("a success on a healthy row is the steady state and writes nothing", () => {
    expect(resolveIntegrationHealthTransition(0, { ok: true }, NOW)).toEqual({ kind: "none" });
  });

  test("a success after a failure clears the row", () => {
    expect(resolveIntegrationHealthTransition(2, { ok: true }, NOW)).toEqual({ kind: "recovery" });
  });

  test("a success on a row whose counter was not loaded defers the decision to the database", () => {
    expect(resolveIntegrationHealthTransition(undefined, { ok: true }, NOW)).toEqual({ kind: "recovery" });
    expect(resolveIntegrationHealthTransition(null, { ok: true }, NOW)).toEqual({ kind: "recovery" });
  });

  test("a failure stamps the moment and the redacted provider text", () => {
    expect(
      resolveIntegrationHealthTransition(0, { ok: false, error: new Error("base not found") }, NOW)
    ).toEqual({ kind: "failure", lastErrorAt: NOW, lastErrorMessage: "base not found" });
  });
});

describe("resolveIntegrationHealthState", () => {
  test("no recorded failure is not a claim that anything was delivered", () => {
    expect(resolveIntegrationHealthState({ lastErrorAt: null, consecutiveFailures: 0 })).toBe(
      "noKnownFailure"
    );
    expect(resolveIntegrationHealthState({ lastErrorAt: null, consecutiveFailures: null })).toBe(
      "noKnownFailure"
    );
  });

  test("a failure below the threshold reads as failing, not as a dead connection", () => {
    expect(
      resolveIntegrationHealthState({
        lastErrorAt: NOW,
        consecutiveFailures: INTEGRATION_RECONNECT_FAILURE_THRESHOLD - 1,
      })
    ).toBe("failing");
  });

  test("an error timestamp with a zero counter still shows as failing", () => {
    expect(resolveIntegrationHealthState({ lastErrorAt: NOW, consecutiveFailures: 0 })).toBe("failing");
  });

  test("the threshold is where the page starts asking for a reconnect", () => {
    expect(
      resolveIntegrationHealthState({
        lastErrorAt: NOW,
        consecutiveFailures: INTEGRATION_RECONNECT_FAILURE_THRESHOLD,
      })
    ).toBe("reconnectSuggested");
  });
});
