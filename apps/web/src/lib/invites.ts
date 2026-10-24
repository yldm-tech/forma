import type { TFunction } from "i18next";

/** Why one invitee of an invite failed: the server's error code for a single invite, a bulk result's failureReason. */
export type InviteFailureReason =
  | "invite_already_exists"
  | "user_already_member"
  | "duplicate_team_ids"
  | "invalid_team_ids"
  | "unknown";

const FAILURE_REASONS: readonly string[] = [
  "invite_already_exists",
  "user_already_member",
  "duplicate_team_ids",
  "invalid_team_ids",
];

export const inviteFailureReason = (code: string): InviteFailureReason =>
  FAILURE_REASONS.includes(code) ? (code as InviteFailureReason) : "unknown";

/** forma2's formatInviteFailureMessage. */
export const inviteFailureMessage = (t: TFunction, email: string, reason: InviteFailureReason): string => {
  switch (reason) {
    case "invite_already_exists":
      return t("workspace.settings.general.invite_failure_invite_exists", { email });
    case "user_already_member":
      return t("workspace.settings.general.invite_failure_already_member", { email });
    case "duplicate_team_ids":
      return t("workspace.settings.general.invite_failure_duplicate_teams", { email });
    case "invalid_team_ids":
      return t("workspace.settings.general.invite_failure_invalid_teams", { email });
    case "unknown":
      return t("workspace.settings.general.invite_failure_unknown", { email });
  }
};

export const isInviteExpired = (invite: { expiresAt: string }, now: Date = new Date()): boolean =>
  now > new Date(invite.expiresAt);
