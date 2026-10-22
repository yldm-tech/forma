import { createFileRoute, redirect } from "@tanstack/react-router";
import { leaveIfOnboardingComplete, onboardingPath } from "../../../../../../features/onboarding/onboarding";

// forma2's AI step sent the user back to the first-survey step whenever AI was unavailable, which it always is here: this server has no AI provider.
export const Route = createFileRoute("/_authenticated/organizations/$organizationId/workspaces/new/ai")({
  beforeLoad: ({ context: { onboarding }, params }) => {
    leaveIfOnboardingComplete(onboarding);
    throw redirect({ href: onboardingPath(params.organizationId, "survey"), replace: true });
  },
});
