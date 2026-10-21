import { describe, expect, test } from "vite-plus/test";
import { ApiError } from "./api";
import { createQueryClient, shouldRetry } from "./query-client";

describe("shouldRetry", () => {
  test.each([400, 401, 403, 404, 409, 429])("never retries a %i", (status) => {
    expect(shouldRetry(0, new ApiError(status, "x", "x"))).toBe(false);
  });

  test("retries server and network failures twice", () => {
    for (const error of [new ApiError(503, "x", "x"), new TypeError("Failed to fetch")]) {
      expect(shouldRetry(0, error)).toBe(true);
      expect(shouldRetry(1, error)).toBe(true);
      expect(shouldRetry(2, error)).toBe(false);
    }
  });
});

test("mutations are never retried", () => {
  expect(createQueryClient().getDefaultOptions().mutations?.retry).toBe(false);
});
