import { beforeEach, describe, expect, test, vi } from "vitest";
import { OperationNotAllowedError, ValidationError } from "@forma/types/errors";
import { updateInviteAction, updateMembershipAction } from "./actions";

const mocks = vi.hoisted(() => ({
  applyRateLimit: vi.fn(),
  assertCan: vi.fn(),
  can: vi.fn(),
  checkAuthorizationUpdated: vi.fn(),
  getAccessControlPermission: vi.fn(),
  getInviteRole: vi.fn(),
  getMembershipByUserIdOrganizationId: vi.fn(),
  getOrganization: vi.fn(),
  getOrganizationIdFromInviteId: vi.fn(),
  getOrganizationOwnerCount: vi.fn(),
  updateInvite: vi.fn(),
  updateMembership: vi.fn(),
}));

vi.mock("@/lib/authorization", () => ({
  assertCan: mocks.assertCan,
  can: mocks.can,
}));

vi.mock("@forma/database", () => ({
  // The last-owner guard runs the owner-count re-check and the update inside one transaction;
  // the fake just invokes the callback with a stand-in tx so both still hit the mocks below.
  prisma: { $transaction: vi.fn((callback: (tx: unknown) => unknown) => callback({})) },
}));

vi.mock("@forma/database/prisma", () => ({
  Prisma: { TransactionIsolationLevel: { Serializable: "Serializable" } },
}));

vi.mock("@/lib/constants", () => ({
  IS_FORMA_CLOUD: true,
  USER_MANAGEMENT_MINIMUM_ROLE: "manager",
}));

vi.mock("@/modules/core/rate-limit/helpers", () => ({
  applyRateLimit: mocks.applyRateLimit,
}));

vi.mock("@/lib/membership/service", () => ({
  getMembershipByUserIdOrganizationId: mocks.getMembershipByUserIdOrganizationId,
}));

vi.mock("@/lib/organization/service", () => ({
  getOrganization: mocks.getOrganization,
}));

vi.mock("@/lib/utils/action-client", () => ({
  authenticatedActionClient: {
    inputSchema: vi.fn(() => ({
      action: vi.fn((fn) => fn),
    })),
  },
}));

vi.mock("@/lib/utils/action-client/action-client-middleware", () => ({
  checkAuthorizationUpdated: mocks.checkAuthorizationUpdated,
}));

vi.mock("@/lib/utils/helper", () => ({
  getOrganizationIdFromInviteId: mocks.getOrganizationIdFromInviteId,
}));

vi.mock("@/modules/audit-logs/lib/handler", () => ({
  withAuditLogging: vi.fn((_eventName, _objectType, fn) => fn),
}));

vi.mock("@/modules/license-check/lib/utils", () => ({
  getAccessControlPermission: mocks.getAccessControlPermission,
}));

vi.mock("@/modules/role-management/lib/invite", () => ({
  getInviteRole: mocks.getInviteRole,
  updateInvite: mocks.updateInvite,
}));

vi.mock("@/modules/role-management/lib/membership", () => ({
  updateMembership: mocks.updateMembership,
}));

vi.mock("@/modules/organization/settings/teams/lib/membership", () => ({
  getOrganizationOwnerCount: mocks.getOrganizationOwnerCount,
}));

const organizationId = "cm9gptbhg0000192zceq9ayuc";
const currentUserId = "cm9gptbhg0001192zceq9ayud";
const targetUserId = "cm9gptbhg0002192zceq9ayue";

const membership = (userId: string, role: string) => ({ userId, organizationId, role, accepted: true });

const callUpdateMembership = (role: string) =>
  updateMembershipAction({
    ctx: { user: { id: currentUserId, locale: "en-US" }, auditLoggingCtx: {} },
    parsedInput: { userId: targetUserId, organizationId, data: { role } },
  } as any);

describe("updateMembershipAction", () => {
  beforeEach(() => {
    vi.clearAllMocks();

    mocks.applyRateLimit.mockResolvedValue(undefined);
    mocks.assertCan.mockResolvedValue(undefined);
    mocks.can.mockResolvedValue(true);
    mocks.checkAuthorizationUpdated.mockResolvedValue(undefined);
    mocks.getAccessControlPermission.mockResolvedValue(true);
    mocks.getOrganization.mockResolvedValue({ id: organizationId });
    mocks.getMembershipByUserIdOrganizationId.mockImplementation(async (userId: string) =>
      membership(userId, "owner")
    );
    mocks.getOrganizationOwnerCount.mockResolvedValue(2);
    mocks.updateMembership.mockImplementation(async (userId: string, _orgId: string, data: any) =>
      membership(userId, data.role)
    );
  });

  test("rejects demoting the last owner of the organization", async () => {
    mocks.getOrganizationOwnerCount.mockResolvedValue(1);

    await expect(callUpdateMembership("member")).rejects.toThrow(ValidationError);
    expect(mocks.updateMembership).not.toHaveBeenCalled();
  });

  test("allows demoting an owner when the organization has another owner", async () => {
    mocks.getOrganizationOwnerCount.mockResolvedValue(2);

    await expect(callUpdateMembership("member")).resolves.toMatchObject({ role: "member" });
    expect(mocks.getOrganizationOwnerCount).toHaveBeenCalledWith(organizationId, expect.anything());
    expect(mocks.updateMembership).toHaveBeenCalledWith(
      targetUserId,
      organizationId,
      { role: "member" },
      expect.anything()
    );
  });

  test("allows changing a non-owner's role", async () => {
    mocks.getMembershipByUserIdOrganizationId.mockImplementation(async (userId: string) =>
      membership(userId, userId === currentUserId ? "owner" : "member")
    );

    await expect(callUpdateMembership("manager")).resolves.toMatchObject({ role: "manager" });
  });

  test("allows keeping an owner's role unchanged", async () => {
    await expect(callUpdateMembership("owner")).resolves.toMatchObject({ role: "owner" });
  });

  test("still rejects a manager demoting an owner before the owner count is read", async () => {
    mocks.getMembershipByUserIdOrganizationId.mockImplementation(async (userId: string) =>
      membership(userId, userId === currentUserId ? "manager" : "owner")
    );

    await expect(callUpdateMembership("member")).rejects.toThrow(OperationNotAllowedError);
    expect(mocks.updateMembership).not.toHaveBeenCalled();
  });
});

describe("updateInviteAction", () => {
  const inviteId = "2f3a1c4e-8d5b-4f7a-9c1e-6b0d2a8f4e31";

  let auditLoggingCtx: Record<string, unknown>;

  const callUpdateInvite = (role: string) => {
    auditLoggingCtx = {};
    return updateInviteAction({
      ctx: { user: { id: currentUserId, locale: "en-US" }, auditLoggingCtx },
      parsedInput: { inviteId, data: { role } },
    } as unknown as Parameters<typeof updateInviteAction>[0]);
  };

  beforeEach(() => {
    vi.clearAllMocks();

    mocks.applyRateLimit.mockResolvedValue(undefined);
    mocks.assertCan.mockResolvedValue(undefined);
    mocks.can.mockResolvedValue(true);
    mocks.getAccessControlPermission.mockResolvedValue(true);
    mocks.getOrganization.mockResolvedValue({ id: organizationId });
    mocks.getOrganizationIdFromInviteId.mockResolvedValue(organizationId);
    mocks.getMembershipByUserIdOrganizationId.mockImplementation(async (userId: string) =>
      membership(userId, "owner")
    );
    mocks.getInviteRole.mockResolvedValue("member");
    mocks.updateInvite.mockResolvedValue(true);
  });

  // The snapshots used to come from `getInvite`, which selects only `email` and the creator's name and is `reactCache`d — so both sides of the update were the same object without a role in it, the diff came out empty, and an escalation to owner left no trace in the audit trail.
  test("records the role on both sides of the change so the diff is not empty", async () => {
    await expect(callUpdateInvite("owner")).resolves.toBe(true);

    expect(mocks.getInviteRole).toHaveBeenCalledWith(inviteId);
    expect(auditLoggingCtx.oldObject).toEqual({ role: "member" });
    expect(auditLoggingCtx.newObject).toEqual({ role: "owner" });
  });

  // The pre-update read has to happen before the write, or it reports the role the invite was just given.
  test("reads the previous role before the invite is updated", async () => {
    const order: string[] = [];
    mocks.getInviteRole.mockImplementation(async () => {
      order.push("read");
      return "member";
    });
    mocks.updateInvite.mockImplementation(async () => {
      order.push("write");
      return true;
    });

    await callUpdateInvite("manager");

    expect(order).toEqual(["read", "write"]);
  });
});
