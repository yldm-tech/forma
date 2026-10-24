import { expect, test } from "vite-plus/test";
import { ApiError } from "./api";
import { errorMessage } from "./errors";
import { initI18n } from "./i18n";

test("errorMessage follows forma2's client error mapping", async () => {
  const { t } = await initI18n("en-US");
  expect(errorMessage(t, new ApiError(429, "too_many_requests", "x", undefined, 30))).toBe(
    "Maximum number of requests reached. Please try again later."
  );
  expect(errorMessage(t, new ApiError(404, "not_found", "Survey not found"))).toBe("Survey not found");
  expect(errorMessage(t, new ApiError(500, "internal_server_error", "boom"))).toBe(
    "Something went wrong. Please try again."
  );
  expect(errorMessage(t, new ApiError(502, "http_error", "Request failed with status 502"))).toBe(
    "Something went wrong. Please try again."
  );
  expect(errorMessage(t, new TypeError("Failed to fetch"))).toBe("Something went wrong. Please try again.");
});
