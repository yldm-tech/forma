/**
 * V3 API workspace → internal IDs translation layer.
 *
 * Workspace is the container for surveys. The workspaceId in the API
 * directly maps to the Workspace entity.
 */
import { ResourceNotFoundError } from "@forma/types/errors";
import { getWorkspace } from "@/lib/workspace/service";

/**
 * Internal IDs derived from a V3 workspace identifier.
 */
export type V3WorkspaceContext = {
  /** Workspace ID — the container for surveys. */
  workspaceId: string;
  /** Organization ID used for org-level auth. */
  organizationId: string;
};

/**
 * Resolves a V3 API workspaceId to internal workspaceId and organizationId.
 *
 * The organization id comes off the row `getWorkspace` already returned. It used to be re-fetched through
 * `getOrganizationIdFromWorkspaceId`, which is itself `getWorkspace(...).organizationId` — and because
 * `getWorkspace`'s React `cache()` wrapper is inert on Route Handler surfaces (no cache dispatcher outside a
 * render pass, see `lib/authzed/outbox-freshness.ts`), that was a second full-row read of `styling`, `config`
 * and `customHeadScripts` on every /api/v3 request, for a field already in hand.
 *
 * @throws ResourceNotFoundError if the workspace does not exist.
 */
export async function resolveV3WorkspaceContext(workspaceId: string): Promise<V3WorkspaceContext> {
  const workspace = await getWorkspace(workspaceId);
  if (!workspace) {
    throw new ResourceNotFoundError("workspace", workspaceId);
  }

  return {
    workspaceId: workspace.id,
    organizationId: workspace.organizationId,
  };
}
