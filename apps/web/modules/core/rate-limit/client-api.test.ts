import { beforeEach, describe, expect, test, vi } from "vitest";

vi.mock("server-only", () => ({}));

const { mocks, constantsMock } = vi.hoisted(() => ({
  mocks: { applyIPRateLimit: vi.fn() },
  constantsMock: { GATEWAY_RATE_LIMITING: false },
}));

vi.mock("@/lib/constants", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(typeof actual === "object" && actual !== null ? actual : {}),
    get GATEWAY_RATE_LIMITING() {
      return constantsMock.GATEWAY_RATE_LIMITING;
    },
  };
});

vi.mock("@/modules/core/rate-limit/helpers", () => ({
  applyIPRateLimit: mocks.applyIPRateLimit,
}));

const { applyClientApiRateLimit } = await import("./client-api");

const post = (pathname: string) => new Request(`https://example.com${pathname}`, { method: "POST" });

beforeEach(() => {
  vi.clearAllMocks();
  constantsMock.GATEWAY_RATE_LIMITING = false;
  mocks.applyIPRateLimit.mockResolvedValue(undefined);
});

describe("applyClientApiRateLimit", () => {
  test("limits response submission when no gateway is declared, which is the default", async () => {
    const result = await applyClientApiRateLimit(post("/api/v2/client/ws_1/responses"));

    expect(result).toBeNull();
    expect(mocks.applyIPRateLimit).toHaveBeenCalledTimes(1);
  });

  test("defers to the gateway on a path its policy set covers", async () => {
    constantsMock.GATEWAY_RATE_LIMITING = true;

    const result = await applyClientApiRateLimit(post("/api/v2/client/ws_1/responses"));

    expect(result).toBeNull();
    expect(mocks.applyIPRateLimit).not.toHaveBeenCalled();
  });

  test("still limits a path the gateway policy does not cover, even with a gateway", async () => {
    constantsMock.GATEWAY_RATE_LIMITING = true;

    const result = await applyClientApiRateLimit(post("/api/v2/client/ws_1/not-in-the-policy-set"));

    expect(result).toBeNull();
    expect(mocks.applyIPRateLimit).toHaveBeenCalledTimes(1);
  });

  test("answers with a response rather than throwing when the caller is over the limit", async () => {
    mocks.applyIPRateLimit.mockRejectedValue(
      Object.assign(new Error("Maximum number of requests reached. Please try again later."), {
        name: "TooManyRequestsError",
      })
    );

    const result = await applyClientApiRateLimit(post("/api/v2/client/ws_1/responses"));

    expect(result).toBeInstanceOf(Response);
  });
});
