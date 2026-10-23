import type { Home } from "./queries/organizations";

/** Where the root page goes: the server's path when it names one, else the workspace this browser last opened when the user can still open it, else their first workspace (forma2's ClientWorkspaceRedirect). Null when there is nowhere to go. */
export const homeTarget = (home: Home, rememberedWorkspaceId: string | null): string | null => {
  if (home.path) return home.path;
  const workspaceId =
    rememberedWorkspaceId && home.workspaceIds.includes(rememberedWorkspaceId)
      ? rememberedWorkspaceId
      : home.workspaceIds[0];
  return workspaceId ? `/workspaces/${workspaceId}/` : null;
};
