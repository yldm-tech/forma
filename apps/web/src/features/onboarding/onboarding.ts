import { queryOptions } from "@tanstack/react-query";
import { redirect } from "@tanstack/react-router";
import { api } from "../../lib/api";

/** GET /api/app/organizations/{id}/onboarding (apps/server/internal/workspace/service.go Onboarding): the workspace the new-workspace flow creates the first survey in, and where to go instead once it has one. Owners and managers only; anyone else gets a not-found, as forma2's workspaces/new layout answered. */
export interface Onboarding {
  workspaceId: string;
  completedPath: string | null;
}

export const onboardingQuery = (organizationId: string) =>
  queryOptions({
    queryKey: ["organizations", organizationId, "onboarding"],
    queryFn: ({ signal }) =>
      api.get<Onboarding>(`/api/app/organizations/${organizationId}/onboarding`, { signal }),
    staleTime: 0,
  });

/** forma2's redirectIfOnboardingComplete: a step of the flow is left for the workspace once the workspace has a survey. */
export const leaveIfOnboardingComplete = (onboarding: Onboarding): void => {
  if (onboarding.completedPath) throw redirect({ href: onboarding.completedPath, replace: true });
};

/** The flow's pages, forma2's /organizations/{id}/workspaces/new/*. */
export const onboardingPath = (
  organizationId: string,
  step: "plan" | "survey" | "templates" | "ai"
): string => `/organizations/${organizationId}/workspaces/new/${step}`;
