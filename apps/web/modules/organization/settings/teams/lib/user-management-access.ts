import "server-only";
import { OperationNotAllowedError } from "@forma/types/errors";
import { can } from "@/lib/authorization";

/**
 * Gate for the organization user-management surface: inviting, removing and re-roling members.
 *
 * This is deliberately a *second* check, asked alongside `organization.manage` rather than instead of
 * it. `organization.manage` is the capability (owner + manager, fixed in the schema);
 * `organization.manage_access` is where the deployment policy lives — the SpiceDB evaluator maps
 * `USER_MANAGEMENT_MINIMUM_ROLE` onto it (`owner` → write, `manager` → manage_access, `disabled` →
 * deny). Asking only the capability means the policy is never consulted, which is what let a manager
 * on an `owner`-restricted install still remove members by POSTing the action: hiding the button is
 * not a gate, and every Server Action is an HTTP endpoint.
 */
export const assertCanManageOrganizationUsers = async (
  userId: string,
  organizationId: string
): Promise<void> => {
  const canManageAccess = await can({ type: "user", id: userId }, "organization.manage_access", {
    type: "organization",
    id: organizationId,
  });

  if (!canManageAccess) {
    throw new OperationNotAllowedError("User management is not allowed for your role");
  }
};
