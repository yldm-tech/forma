import type { QueryClient } from "@tanstack/react-query";
import { ApiError } from "./api";
import { organizationQuery, organizationsQuery } from "./queries/organizations";
import { workspaceQuery } from "./queries/workspaces";
import { WORKSPACE_ID_KEY, readStorage } from "./storage";

/**
 * The organization the account settings pages show in their sidebar (forma2's getSettingsLayoutData): the one of the workspace this browser last opened, while the user can still open it, otherwise their first organization. Null for a user who belongs to none.
 */
export const accountSettingsOrganizationId = async (queryClient: QueryClient): Promise<string | null> => {
  const remembered = readStorage(WORKSPACE_ID_KEY);
  if (remembered) {
    try {
      const workspace = await queryClient.fetchQuery(workspaceQuery(remembered));
      return workspace.organizationId;
    } catch (error) {
      // A workspace the user can no longer open (deleted, access removed, billing role) falls back to the first organization.
      if (!(error instanceof ApiError) || error.status >= 500) throw error;
    }
  }
  const organizations = await queryClient.fetchQuery(organizationsQuery);
  const first = organizations[0];
  if (!first) return null;
  await queryClient.ensureQueryData(organizationQuery(first.id));
  return first.id;
};
