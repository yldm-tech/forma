import { createFileRoute, redirect } from "@tanstack/react-router";
import { onboardingPath } from "../../../../../../features/onboarding/onboarding";

// forma2's plan step chose a Forma Cloud plan before the first survey, and skipped itself off Cloud or for an organization already on a paid plan. Plan selection belongs to the billing module, which this server does not have, so the step always moves on to the first survey, which applies the onboarding gate.
export const Route = createFileRoute("/_authenticated/organizations/$organizationId/workspaces/new/plan")({
  beforeLoad: ({ params }) => {
    throw redirect({ href: onboardingPath(params.organizationId, "survey"), replace: true });
  },
});
