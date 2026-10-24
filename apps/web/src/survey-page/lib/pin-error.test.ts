import { describe, expect, test } from "vite-plus/test";
import { ApiError } from "../../lib/api";
import { pinErrorKind } from "./pin-error";

describe("pinErrorKind", () => {
  test("the server's wrong-PIN answer is an incorrect PIN", () => {
    expect(pinErrorKind(new ApiError(400, "INVALID_PIN", "INVALID_PIN"))).toBe("incorrect");
  });

  test("the attempt limit asks the respondent to wait", () => {
    expect(pinErrorKind(new ApiError(429, "rate_limited", "Too many requests", undefined, 60))).toBe(
      "rateLimited"
    );
  });

  test("a server or proxy failure is not blamed on the PIN", () => {
    expect(pinErrorKind(new ApiError(500, "internal", "Internal error"))).toBe("failed");
    expect(pinErrorKind(new ApiError(502, "http_error", "Request failed with status 502"))).toBe("failed");
  });

  test("a survey that stopped taking responses since the gate loaded is not blamed on the PIN", () => {
    expect(pinErrorKind(new ApiError(404, "not_found", "Survey not found"))).toBe("failed");
  });

  test("another bad request and a network failure are not blamed on the PIN", () => {
    expect(pinErrorKind(new ApiError(400, "bad_request", "pin is required"))).toBe("failed");
    expect(pinErrorKind(new TypeError("Failed to fetch"))).toBe("failed");
  });
});
