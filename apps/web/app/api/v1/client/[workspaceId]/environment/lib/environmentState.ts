import "server-only";
import { createCacheKey } from "@forma/cache";
import { prisma } from "@forma/database";
import { TJsWorkspaceState } from "@forma/types/js";
import {
  addLegacyProjectOverwritesToList,
  addLegacyProjectToEnvironmentState,
} from "@/lib/api/api-backwards-compat";
import { cache } from "@/lib/cache";
import { IS_RECAPTCHA_CONFIGURED, POSTHOG_KEY, RECAPTCHA_SITE_KEY } from "@/lib/constants";
import { capturePostHogEvent } from "@/lib/posthog";
import { getOrganizationIdFromWorkspaceId } from "@/lib/utils/helper";
import { getWorkspaceStateData } from "./data";

/**
 * Optimized environment state fetcher using new caching approach
 * Uses withCache for Redis-backed caching with graceful fallback
 * Single database query via optimized data service
 *
 * @param workspaceId - The workspace ID to fetch state for
 * @returns The environment state
 * @throws ResourceNotFoundError if workspace not found
 */
export const getWorkspaceState = async (
  workspaceId: string
): Promise<{ data: TJsWorkspaceState["data"] }> => {
  return cache.withCache(
    async () => {
      // Single optimized database call replacing multiple service calls
      const { workspace, surveys, actionClasses } = await getWorkspaceStateData(workspaceId);

      // Handle app setup completion update if needed
      // This is a one-time setup flag that can tolerate TTL-based cache expiration.
      // withCache does not dedupe concurrent callers, so the first burst of requests for a workspace that has just embedded the snippet all miss the cache and all read `appSetupCompleted: false`. The write is therefore conditional on the flag still being false, and only the request whose UPDATE actually flipped the row reports the activation — otherwise one activation produces one `app_connected` event per concurrent first page view.
      if (!workspace.appSetupCompleted) {
        const { count: appSetupCompletedCount } = await prisma.workspace.updateMany({
          where: { id: workspaceId, appSetupCompleted: false },
          data: { appSetupCompleted: true },
        });

        if (appSetupCompletedCount > 0 && POSTHOG_KEY) {
          const organizationId = await getOrganizationIdFromWorkspaceId(workspaceId);
          capturePostHogEvent(
            workspaceId,
            "app_connected",
            {
              num_surveys: surveys.length,
              num_code_actions: actionClasses.filter((ac) => ac.type === "code").length,
              num_no_code_actions: actionClasses.filter((ac) => ac.type === "noCode").length,
              organization_id: organizationId ?? "",
              workspace_id: workspaceId,
            },
            organizationId ? { organizationId, workspaceId } : undefined
          );
        }
      }

      // Build the response data
      // Backwards compat: include `project` alongside `workspace`, and
      // `projectOverwrites` alongside `workspaceOverwrites` in each survey
      const data = addLegacyProjectToEnvironmentState({
        surveys: addLegacyProjectOverwritesToList(surveys),
        actionClasses,
        workspace: workspace.workspaceSettings,
        ...(IS_RECAPTCHA_CONFIGURED ? { recaptchaSiteKey: RECAPTCHA_SITE_KEY } : {}),
      } as TJsWorkspaceState["data"]);

      return { data };
    },
    createCacheKey.workspace.state(workspaceId),
    60 * 1000 // 1 minute in milliseconds
  );
};
