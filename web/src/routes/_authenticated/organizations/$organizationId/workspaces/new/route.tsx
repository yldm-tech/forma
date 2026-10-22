import { Outlet, createFileRoute } from "@tanstack/react-router";
import { RoutePending } from "../../../../../../components/shell/route-states";
import { onboardingQuery } from "../../../../../../features/onboarding/onboarding";
import { guardPageError } from "../../../../../../lib/route-guards";

// forma2's workspaces/new layout: the new-workspace flow is for owners and managers (organization.manage), and anyone else gets a not-found. Every step reads the same onboarding state, asked for afresh on each visit.
export const Route = createFileRoute("/_authenticated/organizations/$organizationId/workspaces/new")({
  beforeLoad: async ({ context: { queryClient }, params }) => {
    const onboarding = await queryClient
      .fetchQuery(onboardingQuery(params.organizationId))
      .catch(guardPageError);
    return { onboarding };
  },
  pendingComponent: RoutePending,
  component: Outlet,
});
