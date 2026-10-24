import { createFileRoute, redirect } from "@tanstack/react-router";
import { RoutePending } from "../../../../../../components/shell/route-states";
import { SelectPlanCard } from "../../../../../../features/billing/components/select-plan-card";
import { leaveIfOnboardingComplete, onboardingPath } from "../../../../../../features/onboarding/onboarding";
import { billingSummaryQuery } from "../../../../../../lib/queries/billing";
import { guardPageError } from "../../../../../../lib/route-guards";

// The organization already pays (or has a custom contract) and is not shown the trial.
const PAID_PLANS = new Set(["pro", "scale", "custom"]);

// forma2's plan step: on Forma Cloud, before the first survey, the Pro trial or Hobby. Off Cloud, and for an organization already on a paid plan, the step moves straight on to the first survey.
export const Route = createFileRoute("/_authenticated/organizations/$organizationId/workspaces/new/plan")({
  beforeLoad: async ({ context: { queryClient, onboarding }, params }) => {
    const survey = onboardingPath(params.organizationId, "survey");
    const summary = await queryClient
      .fetchQuery({ ...billingSummaryQuery(params.organizationId), staleTime: 0 })
      .catch(guardPageError);
    if (!summary.isFormaCloud) throw redirect({ href: survey, replace: true });
    leaveIfOnboardingComplete(onboarding);
    if (PAID_PLANS.has(summary.plan)) throw redirect({ href: survey, replace: true });
  },
  pendingComponent: RoutePending,
  component: PlanStep,
});

function PlanStep() {
  const { organizationId } = Route.useParams();
  return (
    <main className="flex w-full flex-1 flex-col items-center justify-center p-6">
      <SelectPlanCard organizationId={organizationId} nextUrl={onboardingPath(organizationId, "survey")} />
    </main>
  );
}
