import { describe, expect, test } from "vitest";
import { getSurveyFollowUpsPermission } from "./utils";

describe("getSurveyFollowUpsPermission", () => {
  test("is on, because follow-ups are part of the product", async () => {
    await expect(getSurveyFollowUpsPermission()).resolves.toBe(true);
  });
});
