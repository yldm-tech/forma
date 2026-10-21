import { beforeEach, describe, expect, test, vi } from "vitest";
import { createOrganizationAction } from "./actions";

const mocks = vi.hoisted(() => ({
  createMembership: vi.fn(),
  createOrganization: vi.fn(),
  createWorkspace: vi.fn(),
  getHasNoOrganizations: vi.fn(),
  getIsMultiOrgEnabled: vi.fn(),
  updateUser: vi.fn(),
}));

vi.mock("@forma/logger", () => ({ logger: { error: vi.fn() } }));

vi.mock("@/lib/constants", () => ({ IS_FORMA_CLOUD: false }));

vi.mock("@/lib/instance/service", () => ({ getHasNoOrganizations: mocks.getHasNoOrganizations }));

vi.mock("@/lib/membership/service", () => ({ createMembership: mocks.createMembership }));

vi.mock("@/lib/organization/service", () => ({ createOrganization: mocks.createOrganization }));

vi.mock("@/lib/posthog", () => ({
  capturePostHogEvent: vi.fn(),
  getEmailDomain: vi.fn(() => "acme.com"),
  groupIdentifyPostHog: vi.fn(),
}));

vi.mock("@/lib/user/service", () => ({ updateUser: mocks.updateUser }));

vi.mock("@/lib/utils/action-client", () => ({
  authenticatedActionClient: {
    inputSchema: vi.fn(() => ({ action: vi.fn((fn) => fn) })),
  },
}));

vi.mock("@/lib/workspace/constants", () => ({ DEFAULT_WORKSPACE_NAME: "My Workspace" }));

vi.mock("@/modules/audit-logs/lib/handler", () => ({
  withAuditLogging: vi.fn((_eventName, _objectType, fn) => fn),
}));

vi.mock("@/modules/billing/lib/organization-billing", () => ({
  ensureCloudStripeSetupForOrganization: vi.fn(),
}));

vi.mock("@/modules/license-check/lib/utils", () => ({ getIsMultiOrgEnabled: mocks.getIsMultiOrgEnabled }));

vi.mock("@/modules/workspaces/settings/lib/workspace", () => ({ createWorkspace: mocks.createWorkspace }));

const buildCtx = (unsubscribedOrganizationIds?: string[]) => ({
  user: {
    id: "user-1",
    email: "admin@acme.com",
    notificationSettings: {
      alert: { "survey-1": true },
      ...(unsubscribedOrganizationIds ? { unsubscribedOrganizationIds } : {}),
    },
  },
  auditLoggingCtx: {} as Record<string, unknown>,
});

describe("createOrganizationAction (first-run setup)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getHasNoOrganizations.mockResolvedValue(true);
    mocks.getIsMultiOrgEnabled.mockResolvedValue(false);
    mocks.createOrganization.mockResolvedValue({ id: "org-1", name: "Acme" });
    mocks.createWorkspace.mockResolvedValue({ id: "ws-1", name: "My Workspace" });
  });

  test("opts the creator out of per-response alerts for the organization it just created", async () => {
    await createOrganizationAction({
      ctx: buildCtx(),
      parsedInput: { organizationName: "Acme" },
    } as never);

    // Same opt-out every other organization-creation path applies; without it the first-run admin
    // gets one email per survey response.
    expect(mocks.updateUser).toHaveBeenCalledWith("user-1", {
      notificationSettings: {
        alert: { "survey-1": true },
        unsubscribedOrganizationIds: ["org-1"],
      },
    });
  });

  test("keeps the organizations the creator was already unsubscribed from", async () => {
    await createOrganizationAction({
      ctx: buildCtx(["org-0"]),
      parsedInput: { organizationName: "Acme" },
    } as never);

    expect(mocks.updateUser).toHaveBeenCalledWith("user-1", {
      notificationSettings: {
        alert: { "survey-1": true },
        unsubscribedOrganizationIds: ["org-0", "org-1"],
      },
    });
  });
});
