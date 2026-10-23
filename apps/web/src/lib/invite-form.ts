import type { OrganizationRole } from "./roles";
import { emailError, userNameError } from "./validation";

export interface InviteForm {
  name: string;
  email: string;
  role: OrganizationRole;
  teamIds: string[];
}

export type InviteFormErrors = Partial<Record<"name" | "email" | "teamIds", string>>;

/** forma2's individual invite form schema: ZUserName, an email, and for a team admin (who invites into their own teams only) at least one team. */
export const validateInviteForm = (form: InviteForm, teamAdminOnly: boolean): InviteFormErrors => {
  const errors: InviteFormErrors = {};
  const name = userNameError(form.name);
  if (name) errors.name = name;
  const email = emailError(form.email);
  if (email) errors.email = email;
  if (teamAdminOnly && form.teamIds.length === 0)
    errors.teamIds = "Team admins must select at least one team";
  return errors;
};
