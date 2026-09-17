import { beforeEach, describe, expect, test, vi } from "vitest";
import type { TOrganizationBilling } from "@forma/types/organizations";

// The unlock only fires when Stripe resolved no features, so every fixture here has none unless the test is about the opposite case. Everything else is mocked to keep the provider from reaching Stripe, a database or the licence server — none of which the branch under test touches.
const { constantsMock } = vi.hoisted(() => ({
  constantsMock: { IS_DEVELOPMENT: false, E2E_TESTING: false },
}));

vi.mock("server-only", () => ({}));

vi.mock("@/lib/constants", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(typeof actual === "object" && actual !== null ? actual : {}),
    get IS_DEVELOPMENT() {
      return constantsMock.IS_DEVELOPMENT;
    },
    get E2E_TESTING() {
      return constantsMock.E2E_TESTING;
    },
  };
});

vi.mock("@forma/logger", () => ({ logger: { warn: vi.fn() } }));

vi.mock("@/modules/ee/billing/lib/organization-billing", () => ({
  getOrganizationBillingWithReadThroughSync: vi.fn(),
  getDefaultOrganizationBilling: () => ({
    limits: { workspaces: 1, monthly: { responses: 250, workflowRuns: null } },
    stripeCustomerId: null,
    usageCycleAnchor: null,
  }),
}));

vi.mock("@/modules/ee/license-check/lib/license", () => ({ getEnterpriseLicense: vi.fn() }));

const { getOrganizationBillingWithReadThroughSync } = await import(
  "@/modules/ee/billing/lib/organization-billing"
);
const { getEnterpriseLicense } = await import("@/modules/ee/license-check/lib/license");
const { getCloudOrganizationEntitlementsContext } = await import("./cloud-provider");
const { KNOWN_ENTITLEMENT_FEATURES } = await import("./types");

const mockGetBilling = vi.mocked(getOrganizationBillingWithReadThroughSync);
const mockGetLicense = vi.mocked(getEnterpriseLicense);

const createBillingFixture = (overrides: Partial<TOrganizationBilling> = {}): TOrganizationBilling => ({
  stripeCustomerId: null,
  limits: { workspaces: 3, monthly: { responses: 1500, workflowRuns: null } },
  usageCycleAnchor: null,
  ...overrides,
});

// What the licence path returns on a development install with no key (license.ts), which is the
// state this unlock exists to keep working once IS_FORMA_CLOUD is set.
const developmentLicense = { status: "active", active: true, features: { contacts: true } } as never;

beforeEach(() => {
  vi.clearAllMocks();
  constantsMock.IS_DEVELOPMENT = false;
  constantsMock.E2E_TESTING = false;
  mockGetLicense.mockResolvedValue(developmentLicense);
});

describe("cloud entitlements with no Stripe subscription", () => {
  test("stays locked when NODE_ENV says nothing about development", async () => {
    mockGetBilling.mockResolvedValue(createBillingFixture());

    const result = await getCloudOrganizationEntitlementsContext("org1");

    expect(result.features).toEqual([]);
    expect(result.limits).toEqual({ workspaces: 3, monthlyResponses: 1500, monthlyWorkflowRuns: null });
  });

  test("stays locked with no billing record either, so production keeps the hobby defaults", async () => {
    mockGetBilling.mockResolvedValue(null);

    const result = await getCloudOrganizationEntitlementsContext("org1");

    expect(result.features).toEqual([]);
    expect(result.limits).toEqual({ workspaces: 1, monthlyResponses: 250, monthlyWorkflowRuns: null });
  });

  test("grants every entitlement in development, so setting IS_FORMA_CLOUD does not cost the licensed features", async () => {
    constantsMock.IS_DEVELOPMENT = true;
    mockGetBilling.mockResolvedValue(createBillingFixture());

    const result = await getCloudOrganizationEntitlementsContext("org1");

    expect(result.features).toEqual([...KNOWN_ENTITLEMENT_FEATURES]);
    // The four keys with no licence feature behind them: self-hosted grants these unconditionally.
    expect(result.features).toContain("follow-ups");
    expect(result.features).toContain("custom-links-in-surveys");
    expect(result.features).toContain("custom-redirect-url");
    expect(result.features).toContain("bulk-invite");
  });

  test("grants them with no billing record too", async () => {
    constantsMock.IS_DEVELOPMENT = true;
    mockGetBilling.mockResolvedValue(null);

    const result = await getCloudOrganizationEntitlementsContext("org1");

    expect(result.features).toEqual([...KNOWN_ENTITLEMENT_FEATURES]);
  });

  test("lifts the limits, so a local cloud instance is not capped at one workspace", async () => {
    constantsMock.IS_DEVELOPMENT = true;
    mockGetBilling.mockResolvedValue(null);

    const result = await getCloudOrganizationEntitlementsContext("org1");

    expect(result.limits).toEqual({
      workspaces: null,
      monthlyResponses: null,
      monthlyWorkflowRuns: null,
    });
  });

  test("unlocks under E2E, which is how the browser suite gets a licensed app", async () => {
    constantsMock.E2E_TESTING = true;
    mockGetBilling.mockResolvedValue(createBillingFixture());

    const result = await getCloudOrganizationEntitlementsContext("org1");

    expect(result.features).toEqual([...KNOWN_ENTITLEMENT_FEATURES]);
  });

  test("leaves a real subscription alone in development, so the cloud path stays testable", async () => {
    constantsMock.IS_DEVELOPMENT = true;
    mockGetBilling.mockResolvedValue(
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
