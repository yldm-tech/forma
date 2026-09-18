import { describe, expect, test } from "vitest";
import { getBillingFallbackPath } from "./navigation";

describe("getBillingFallbackPath", () => {
  test("returns billing settings path for cloud", () => {
    const path = getBillingFallbackPath("org_123", true);
    expect(path).toBe("/organizations/org_123/settings/billing");
  });

  // Off Cloud there is no organization page a billing role can open — every one of them needs
  // `organization.read_access`, which that role does not have — so the fallback leaves the
  // organization entirely rather than pointing at a page that would bounce it straight back.
  test("returns the user's own account settings for self-hosted", () => {
    const path = getBillingFallbackPath("org_123", false);
    expect(path).toBe("/account/settings/profile");
  });
});
