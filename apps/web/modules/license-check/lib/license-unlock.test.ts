import { describe, expect, test, vi } from "vitest";

vi.mock("server-only", () => ({}));

const { getEnterpriseLicense, getLicenseFeatures } = await import("./license");

describe("the licence this build reports", () => {
  test("is active and carries every feature", async () => {
    const result = await getEnterpriseLicense();

    expect(result.active).toBe(true);
    expect(result.status).toBe("active");
    expect(result.features?.contacts).toBe(true);
    expect(result.features?.workflows).toBe(true);
    expect(result.features?.quotas).toBe(true);
    expect(result.features?.sso).toBe(true);
    // `workspaces` is a limit rather than a flag; null means unlimited.
    expect(result.features?.workspaces).toBeNull();
  });

  test("leaves no feature flagged off, so an install is never half-featured", async () => {
    const { features } = await getEnterpriseLicense();

    expect(Object.entries(features ?? {}).filter(([, value]) => value === false)).toEqual([]);
  });

  test("schedules no downgrade, so nothing can take a feature away later", async () => {
    const result = await getEnterpriseLicense();

    expect(result.isPendingDowngrade).toBe(false);
  });

  test("exposes the same features through getLicenseFeatures", async () => {
    const [{ features }, direct] = await Promise.all([getEnterpriseLicense(), getLicenseFeatures()]);

    expect(direct).toEqual(features);
  });
});
