import { beforeEach, describe, expect, test, vi } from "vitest";
import { logger } from "@forma/logger";
import { getMembershipByUserIdOrganizationId } from "@/lib/membership/service";
import { logSignOut } from "@/modules/auth/lib/utils";
import { logSignOutAction } from "./sign-out";

vi.mock("@forma/logger", () => ({
  logger: {
    error: vi.fn(),
  },
}));

vi.mock("@/modules/auth/lib/utils", () => ({
  logSignOut: vi.fn(),
}));

vi.mock("@/lib/membership/service", () => ({
  getMembershipByUserIdOrganizationId: vi.fn(),
}));

vi.mock("@/lib/utils/action-client", () => ({
  authenticatedActionClient: {
    inputSchema: vi.fn(() => ({
      action: vi.fn((fn) => fn),
    })),
  },
}));

// Clear the existing mock from vitestSetup.ts
vi.unmock("@/modules/auth/actions/sign-out");

const sessionUser = { id: "user123", email: "session@example.com" };

// The mocked action client passes the handler through untouched, so it is invoked with the shape the
// real client would build.
const invoke = (parsedInput: Record<string, unknown>) =>
  (logSignOutAction as unknown as (args: unknown) => Promise<void>)({
    ctx: { user: sessionUser },
    parsedInput,
  });

describe("logSignOutAction", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(getMembershipByUserIdOrganizationId).mockResolvedValue({ role: "owner" } as never);
  });

  test("records the session user as the actor, never one supplied by the caller", async () => {
    // This is a Server Action, so anyone able to load the app can POST to it. Taking the id from the
    // body let a caller write `userSignedOut` rows naming somebody else, indistinguishable from real
    // ones, which costs the audit log its value as evidence.
    await invoke({
      reason: "user_initiated",
      redirectUrl: "https://example.com",
      organizationId: "org123",
      userId: "somebody-else",
      userEmail: "attacker@example.com",
    });

    expect(logSignOut).toHaveBeenCalledWith(sessionUser.id, sessionUser.email, {
      reason: "user_initiated",
      redirectUrl: "https://example.com",
      organizationId: "org123",
    });
    expect(logSignOut).toHaveBeenCalledTimes(1);
  });

  test("drops an organizationId the session user does not belong to", async () => {
    vi.mocked(getMembershipByUserIdOrganizationId).mockResolvedValue(null as never);

    await invoke({ reason: "user_initiated", organizationId: "someone-elses-org" });

    expect(logSignOut).toHaveBeenCalledWith(
      sessionUser.id,
      sessionUser.email,
      expect.objectContaining({ organizationId: undefined })
    );
  });

  test("does not look up a membership when no organizationId is given", async () => {
    await invoke({ reason: "session_timeout" });

    expect(getMembershipByUserIdOrganizationId).not.toHaveBeenCalled();
    expect(logSignOut).toHaveBeenCalledWith(
      sessionUser.id,
      sessionUser.email,
      expect.objectContaining({ reason: "session_timeout", organizationId: undefined })
    );
  });

  test("logs and rethrows when the audit write fails", async () => {
    const mockError = new Error("Failed to log sign out");
    vi.mocked(logSignOut).mockImplementation(() => {
      throw mockError;
    });

    await expect(invoke({ reason: "user_initiated" })).rejects.toThrow(mockError);

    expect(logger.error).toHaveBeenCalledWith(
      {
        userId: sessionUser.id,
        error: mockError.message,
      },
      "Failed to log sign out event"
    );
    expect(logger.error).toHaveBeenCalledTimes(1);
  });

  test("does not log an error when the audit write succeeds", async () => {
    await invoke({ reason: "user_initiated" });

    expect(logger.error).not.toHaveBeenCalled();
  });
});
