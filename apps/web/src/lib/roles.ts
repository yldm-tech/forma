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
  userId: string;
  role: OrganizationRole;
  isActive: boolean;
  suspendedAt?: string | null;
}

/** Mirrors the server's owner guards, which count the owners other than the member acted on who can still act in the organization: neither a pending owner invite, a deactivated owner nor a suspended one counts. With userId undefined (an invite row) every active owner counts. */
export const hasAnotherActiveOwner = (members: readonly MemberLike[], userId: string | undefined): boolean =>
  members.some(
    (member) => member.userId !== userId && member.role === "owner" && member.isActive && !member.suspendedAt
  );

export interface RoleEditContext {
  isUserManagementDisabled: boolean;
  currentUserRole: OrganizationRole | undefined;
  currentUserId: string;
  memberRole: OrganizationRole;
  /** The target's user id on a membership row; undefined on an invite row. */
  memberUserId?: string;
  /** Another owner than the target can still act in the organization (hasAnotherActiveOwner). */
  hasAnotherActiveOwner: boolean;
}

/** forma2's isRoleEditDisabled: never your own role, never the last active owner's (an invite is not a membership, so the rule skips invites), and a manager never touches an owner. */
export const isRoleEditDisabled = (ctx: RoleEditContext): boolean =>
  ctx.isUserManagementDisabled ||
  ctx.memberUserId === ctx.currentUserId ||
  (ctx.memberUserId !== undefined && ctx.memberRole === "owner" && !ctx.hasAnotherActiveOwner) ||
  (ctx.currentUserRole === "manager" && ctx.memberRole === "owner");

export interface DeleteContext {
  currentUserRole: OrganizationRole | undefined;
  currentUserId: string;
  /** Another owner than the target can still act in the organization (hasAnotherActiveOwner). */
  hasAnotherActiveOwner: boolean;
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
  if (row.role === "owner") return ctx.hasAnotherActiveOwner;
  return true;
};

/** Whether the caller may suspend a member's access to the organization, or restore it, by the server's rules (those of a role change): only under user management, never their own, a manager never an owner's, and an owner is suspended only while another active owner remains. Restoring needs no other owner. */
export const canChangeSuspension = (
  member: { userId: string; role: OrganizationRole; suspended: boolean },
  ctx: DeleteContext & { canManageUsers: boolean }
): boolean => {
  const { isOwnerOrManager, isManager } = accessFlags(ctx.currentUserRole);
  if (!ctx.canManageUsers || !isOwnerOrManager) return false;
  if (member.userId === ctx.currentUserId) return false;
  if (isManager && member.role === "owner") return false;
  if (member.role === "owner" && !member.suspended) return ctx.hasAnotherActiveOwner;
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
