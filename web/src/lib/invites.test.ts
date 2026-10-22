import { expect, test } from "vite-plus/test";
import { initI18n } from "./i18n";
import { inviteFailureMessage, inviteFailureReason, isInviteExpired } from "./invites";

test("inviteFailureReason maps the server's codes and nothing else", () => {
  expect(inviteFailureReason("invite_already_exists")).toBe("invite_already_exists");
  expect(inviteFailureReason("user_already_member")).toBe("user_already_member");
  expect(inviteFailureReason("invalid_team_ids")).toBe("invalid_team_ids");
  expect(inviteFailureReason("duplicate_team_ids")).toBe("duplicate_team_ids");
  expect(inviteFailureReason("forbidden")).toBe("unknown");
});

test("inviteFailureMessage names the address", async () => {
  const { t } = await initI18n("en-US");
  for (const reason of ["invite_already_exists", "user_already_member", "unknown"] as const) {
    const message = inviteFailureMessage(t, "ada@example.com", reason);
    expect(message).toContain("ada@example.com");
    expect(message).not.toContain("workspace.settings");
  }
});

test("isInviteExpired", () => {
  const now = new Date("2026-10-22T12:00:00Z");
  expect(isInviteExpired({ expiresAt: "2026-10-22T11:59:59Z" }, now)).toBe(true);
  expect(isInviteExpired({ expiresAt: "2026-10-29T12:00:00Z" }, now)).toBe(false);
});
