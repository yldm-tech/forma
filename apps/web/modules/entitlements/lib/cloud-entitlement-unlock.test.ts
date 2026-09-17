import { beforeEach, describe, expect, test, vi } from "vitest";
import type { TOrganizationBilling } from "@forma/types/organizations";

// The ungated branch is the one where Stripe resolved no features, so every fixture here has none unless the test is about the opposite case. Everything else is mocked to keep the provider from reaching Stripe, a database or a licence server.
vi.mock("server-only", () => ({}));

vi.mock("@forma/logger", () => ({ logger: { warn: vi.fn() } }));

vi.mock("@/modules/ee/billing/lib/organization-billing", () => ({
  getOrganizationBillingWithReadThroughSync: vi.fn(),
}));

vi.mock("@/modules/ee/license-check/lib/license", () => ({ getEnterpriseLicense: vi.fn() }));

const { getOrganizationBillingWithReadThroughSync } =
  await import("@/modules/ee/billing/lib/organization-billing");
const { getEnterpriseLicense } = await import("@/modules/ee/license-check/lib/license");
const { getCloudOrganizationEntitlementsContext } = await import("./cloud-provider");
const { KNOWN_ENTITLEMENT_FEATURES } = await import("./types");

const createBillingFixture = (overrides: Partial<TOrganizationBilling> = {}): TOrganizationBilling => ({
  stripeCustomerId: null,
  limits: { workspaces: 3, monthly: { responses: 1500, workflowRuns: null } },
  usageCycleAnchor: null,
  ...overrides,
});

const ungatedLicense = { status: "active", active: true, features: { contacts: true } } as never;

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(getEnterpriseLicense).mockResolvedValue(ungatedLicense);
});

describe("cloud entitlements with no Stripe subscription", () => {
  test("grants every entitlement, so IS_FORMA_CLOUD does not turn the product off", async () => {
    vi.mocked(getOrganizationBillingWithReadThroughSync).mockResolvedValue(createBillingFixture());

    const result = await getCloudOrganizationEntitlementsContext("org1");

    expect(result.features).toEqual([...KNOWN_ENTITLEMENT_FEATURES]);
    // The four keys with no licence feature behind them, which self-hosted grants unconditionally.
    expect(result.features).toContain("follow-ups");
    expect(result.features).toContain("custom-links-in-surveys");
    expect(result.features).toContain("custom-redirect-url");
    expect(result.features).toContain("bulk-invite");
  });

  test("grants them with no billing record either", async () => {
    vi.mocked(getOrganizationBillingWithReadThroughSync).mockResolvedValue(null);

    const result = await getCloudOrganizationEntitlementsContext("org1");

    expect(result.features).toEqual([...KNOWN_ENTITLEMENT_FEATURES]);
  });

  test("lifts the limits, so an organization is not capped at one workspace", async () => {
    vi.mocked(getOrganizationBillingWithReadThroughSync).mockResolvedValue(createBillingFixture());

    const result = await getCloudOrganizationEntitlementsContext("org1");

    expect(result.limits).toEqual({
      workspaces: null,
      monthlyResponses: null,
      monthlyWorkflowRuns: null,
    });
  });

  test("leaves a real subscription alone, so the cloud path stays reachable", async () => {
    vi.mocked(getOrganizationBillingWithReadThroughSync).mockResolvedValue(
      createBillingFixture({
        stripeCustomerId: "cus_1",
        stripe: { features: ["contacts"], subscriptionStatus: "active" },
      } as Partial<TOrganizationBilling>)
    );

    const result = await getCloudOrganizationEntitlementsContext("org1");

    expect(result.features).toEqual(["contacts"]);
    expect(result.limits).toEqual({ workspaces: 3, monthlyResponses: 1500, monthlyWorkflowRuns: null });
  });
});
