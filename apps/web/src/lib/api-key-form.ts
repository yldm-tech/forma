import type { ApiKey, ApiKeyPermission, OrganizationAccess } from "./queries/api-keys";

export interface PermissionRow {
  workspaceId: string;
  permission: ApiKeyPermission;
}

/** forma2's checkForDuplicatePermissions: a workspace may be granted once. */
export const hasDuplicateWorkspace = (rows: readonly PermissionRow[]): boolean =>
  new Set(rows.map((row) => row.workspaceId)).size !== rows.length;

/** forma2's isSubmitDisabled: a label that is not blank, and at least one workspace grant or organization access switch. */
export const canSubmitApiKey = (
  label: string,
  rows: readonly PermissionRow[],
  access: OrganizationAccess
): boolean =>
  label.trim() !== "" && (rows.length > 0 || access.accessControl.read || access.accessControl.write);

/** Write implies read: the read switch shows on, and is sent on, whenever write is. */
export const effectiveAccess = (access: OrganizationAccess): OrganizationAccess => ({
  accessControl: {
    read: access.accessControl.read || access.accessControl.write,
    write: access.accessControl.write,
  },
});

/** What a key may reach, as the API keys list summarises it in one line. */
export interface ApiKeyScope {
  /** The first workspace grants, in the server's order (by workspace name). */
  shown: ApiKey["workspacePermissions"];
  /** How many further workspace grants the line leaves out. */
  more: number;
  /** The organization access the key holds, write including read; null for none. */
  organization: "read" | "write" | null;
}

/** A key's scope for the list: at most limit workspace grants by name, the count of the rest, and its organization access read the way the server reads it (write implies read). */
export const apiKeyScope = (
  key: Pick<ApiKey, "workspacePermissions" | "organizationAccess">,
  limit = 2
): ApiKeyScope => {
  const access = effectiveAccess(key.organizationAccess).accessControl;
  return {
    shown: key.workspacePermissions.slice(0, limit),
    more: Math.max(0, key.workspacePermissions.length - limit),
    organization: access.write ? "write" : access.read ? "read" : null,
  };
};
