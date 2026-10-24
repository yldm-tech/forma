import { emailError, userNameError } from "./validation";

/** forma2 bounded the onboarding invite form so one submit cannot fan out an unlimited number of owner invites. */
export const MAX_SETUP_INVITES = 10;

export interface SetupInviteRow {
  email: string;
  name: string;
}

export type SetupInviteErrors = { email?: string; name?: string }[];

/** forma2's ZInviteMembersFormSchema: every row shown needs a valid address ("Invalid email address") and a ZUserName; skipping the step is its own button. */
export const validateSetupInvites = (
  rows: readonly SetupInviteRow[]
): { invitees: SetupInviteRow[]; errors: SetupInviteErrors } => {
  const errors = rows.map((row) => {
    const email = emailError(row.email) ? "Invalid email address" : null;
    const name = userNameError(row.name);
    return { ...(email ? { email } : {}), ...(name ? { name } : {}) };
  });
  const invitees = rows.map((row) => ({ email: row.email.trim().toLowerCase(), name: row.name.trim() }));
  return { invitees, errors };
};
