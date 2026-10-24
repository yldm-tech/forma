// forma2's organization and team role rules as the members and teams pages apply them. The server enforces every one of them again; these decide what the pages offer.

import type { TFunction } from "i18next";

export type OrganizationRole = "owner" | "manager" | "member" | "billing";

/** An organization role as the pages name it, in the user's language; the enum value itself is never shown. */
export const roleLabel = (t: TFunction, role: OrganizationRole): string => {
  switch (role) {
    case "owner":
      return t("workspace.settings.teams.role_owner");
    case "manager":
      return t("workspace.settings.teams.role_manager");
    case "member":
      return t("workspace.settings.teams.role_member");
    case "billing":
      return t("workspace.settings.teams.role_billing");
  }
};

export interface AccessFlags {
  isOwner: boolean;
  isManager: boolean;
  isMember: boolean;
  isBilling: boolean;
  isOwnerOrManager: boolean;
}

/** forma2's getAccessFlags; an unknown role (still loading) has no flags. */
export const accessFlags = (role: OrganizationRole | "" | undefined): AccessFlags => ({
  isOwner: role === "owner",
  isManager: role === "manager",
  isMember: role === "member",
  isBilling: role === "billing",
  isOwnerOrManager: role === "owner" || role === "manager",
});

/** The roles a member or invite can be given by this caller, in the order the menu lists them: owners may assign every role (billing only where the instance allows it), managers only member. */
export const assignableRoles = (
  currentRole: OrganizationRole | undefined,
  billingRoleAllowed: boolean
): OrganizationRole[] => {
  if (currentRole !== "owner") return ["member"];
  return billingRoleAllowed ? ["billing", "owner", "manager", "member"] : ["owner", "manager", "member"];
};

export interface MemberLike {
  role: OrganizationRole;
  isActive: boolean;
}

/** Mirrors the server's last-owner guard, which counts owner memberships whose user can still sign in: neither a pending owner invite nor a deactivated owner counts. */
export const hasMoreThanOneActiveOwner = (members: readonly MemberLike[]): boolean =>
  members.filter((member) => member.role === "owner" && member.isActive).length > 1;

export interface RoleEditContext {
  isUserManagementDisabled: boolean;
  currentUserRole: OrganizationRole | undefined;
  currentUserId: string;
  memberRole: OrganizationRole;
  /** The target's user id on a membership row; undefined on an invite row. */
  memberUserId?: string;
  doesOrgHaveMoreThanOneOwner: boolean;
}

/** forma2's isRoleEditDisabled: never your own role, never the last owner's (an invite is not a membership, so the rule skips invites), and a manager never touches an owner. */
export const isRoleEditDisabled = (ctx: RoleEditContext): boolean =>
  ctx.isUserManagementDisabled ||
  ctx.memberUserId === ctx.currentUserId ||
  (ctx.memberUserId !== undefined && ctx.memberRole === "owner" && !ctx.doesOrgHaveMoreThanOneOwner) ||
  (ctx.currentUserRole === "manager" && ctx.memberRole === "owner");

export interface DeleteContext {
  currentUserRole: OrganizationRole | undefined;
  currentUserId: string;
  doesOrgHaveMoreThanOneOwner: boolean;
}

/** forma2's showDeleteButton: owners and managers revoke invites; nobody removes themselves; a manager never removes an owner; an owner removes another owner only while another active owner remains. */
export const canDeleteRow = (
  row: { kind: "invite" } | { kind: "member"; userId: string; role: OrganizationRole },
  ctx: DeleteContext
): boolean => {
  const { isOwnerOrManager, isManager } = accessFlags(ctx.currentUserRole);
  if (!isOwnerOrManager) return false;
  if (row.kind === "invite") return true;
  if (row.userId === ctx.currentUserId) return false;
  if (isManager) return row.role !== "owner";
  if (row.role === "owner") return ctx.doesOrgHaveMoreThanOneOwner;
  return true;
};

/** forma2's isManageDisabled on the teams table: owners and managers manage any team, anyone else only a team they administer. */
export const canManageTeam = (
  team: { userRole?: "admin" | "contributor" },
  isOwnerOrManager: boolean
): boolean => isOwnerOrManager || team.userRole === "admin";

/** The team role a member gets when added to a team: owners and managers administer every team they are in. */
export const defaultTeamRole = (role: OrganizationRole | undefined): "admin" | "contributor" =>
  role === "owner" || role === "manager" ? "admin" : "contributor";
