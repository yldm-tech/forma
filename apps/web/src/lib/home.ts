import type { Home } from "./queries/organizations";
import { readStorage, writeStorage } from "./storage";

const onboardingSkippedKey = (organizationId: string) => `forma.onboardingSkipped.${organizationId}`;

/** Remembers in this browser that the user chose "Skip for now" in the organization's onboarding, so the root page stops sending them back to it. */
export const skipOnboarding = (organizationId: string): void =>
  writeStorage(onboardingSkippedKey(organizationId), "1");

export const isOnboardingSkipped = (organizationId: string): boolean =>
  readStorage(onboardingSkippedKey(organizationId)) === "1";

const ONBOARDING_PATH = /^\/organizations\/([^/]+)\/workspaces\/new\//;

/** Where the root page goes: the server's path when it names one, else the workspace this browser last opened when the user can still open it, else their first workspace (forma2's ClientWorkspaceRedirect). The server's onboarding path is passed over for an organization whose onboarding the user skipped in this browser, to their workspaces or, with none, the organization itself. Null when there is nowhere to go. */
export const homeTarget = (
  home: Home,
  rememberedWorkspaceId: string | null,
  skipped: (organizationId: string) => boolean = isOnboardingSkipped
): string | null => {
  const skippedOrganization = home.path?.match(ONBOARDING_PATH)?.[1];
  if (home.path && !(skippedOrganization && skipped(skippedOrganization))) return home.path;
  const workspaceId =
    rememberedWorkspaceId && home.workspaceIds.includes(rememberedWorkspaceId)
      ? rememberedWorkspaceId
      : home.workspaceIds[0];
  if (workspaceId) return `/workspaces/${workspaceId}/`;
  return skippedOrganization ? `/organizations/${skippedOrganization}/` : null;
};
