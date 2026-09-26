import { beforeEach, describe, expect, test, vi } from "vitest";
import { inviteOrganizationMemberAction } from "./actions";

const mocks = vi.hoisted(() => ({
  applyInviteRateLimit: vi.fn(),
  checkSetupInviteAuthorization: vi.fn(),
  inviteUser: vi.fn(),
  sendInviteMemberEmail: vi.fn(),
}));

vi.mock("@/lib/constants", () => ({ INVITE_DISABLED: false }));

vi.mock("@/lib/utils/action-client", () => ({
  authenticatedActionClient: {
    inputSchema: vi.fn(() => ({ action: vi.fn((fn) => fn) })),
  },
}));

vi.mock("@/modules/audit-logs/lib/handler", () => ({
  withAuditLogging: vi.fn((_eventName, _objectType, fn) => fn),
}));

vi.mock("@/modules/email", () => ({ sendInviteMemberEmail: mocks.sendInviteMemberEmail }));

vi.mock("@/modules/organization/settings/teams/lib/invite-rate-limit", () => ({
  applyInviteRateLimit: mocks.applyInviteRateLimit,
}));

vi.mock("@/modules/setup/organization/[organizationId]/invite/lib/authorization", () => ({
  checkSetupInviteAuthorization: mocks.checkSetupInviteAuthorization,
}));

vi.mock("@/modules/setup/organization/[organizationId]/invite/lib/invite", () => ({
  inviteUser: mocks.inviteUser,
}));

const ctx = {
  user: { id: "user-1", name: "Admin User" },
  auditLoggingCtx: {} as Record<string, unknown>,
};

describe("inviteOrganizationMemberAction", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    ctx.auditLoggingCtx = {};
    mocks.inviteUser.mockResolvedValue("invite-1");
  });

  test("sends the invite email with the invitee name the form collected", async () => {
    await inviteOrganizationMemberAction({
      ctx,
      parsedInput: { email: "dana@acme.com", organizationId: "org-1", name: "Dana Reed" },
    } as never);

    // The email heading renders `Hey {inviteeName}`, so an empty name ships "Hey " to the first
    // people an instance ever invites.
    expect(mocks.sendInviteMemberEmail).toHaveBeenCalledWith(
      "invite-1",
      "dana@acme.com",
      "Admin User",
      "Dana Reed"
    );
  });
});
