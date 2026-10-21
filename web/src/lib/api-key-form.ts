import type { ApiKeyPermission, OrganizationAccess } from "./queries/api-keys";

export interface PermissionRow {
  workspaceId: string;
  permission: ApiKeyPermission;
}

/** forma2's checkForDuplicatePermissions: a workspace may be granted once. */
export const hasDuplicateWorkspace = (rows: readonly PermissionRow[]): boolean =>
  new Set(rows.map((row) => row.workspaceId)).size !== rows.length;

/** forma2's isSubmitDisabled: a label that is not blank, and at least one workspace grant or organization access switch. */
export const canSubmitApiKey = (label: string, rows: readonly PermissionRow[], access: OrganizationAccess): boolean =>
  label.trim() !== "" && (rows.length > 0 || access.accessControl.read || access.accessControl.write);

/** Write implies read: the read switch shows on, and is sent on, whenever write is. */
export const effectiveAccess = (access: OrganizationAccess): OrganizationAccess => ({
  accessControl: {
    read: access.accessControl.read || access.accessControl.write,
    write: access.accessControl.write,
  },
});
