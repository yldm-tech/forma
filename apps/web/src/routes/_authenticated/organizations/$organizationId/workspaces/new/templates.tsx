import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { RoutePending } from "../../../../../../components/shell/route-states";
import { XMTemplateList } from "../../../../../../features/onboarding/first-survey";
import { leaveIfOnboardingComplete } from "../../../../../../features/onboarding/onboarding";
import {
  resolveDefaultSurveyLanguage,
  workspaceSurveyConfig,
} from "../../../../../../features/surveys/defaults";
import { meQuery } from "../../../../../../lib/queries/auth";
import { workspaceQuery } from "../../../../../../lib/queries/workspaces";
import { guardPageError } from "../../../../../../lib/route-guards";

// forma2's onboarding template step: the experience-management templates, left for the workspace once it has a survey.
export const Route = createFileRoute(
  "/_authenticated/organizations/$organizationId/workspaces/new/templates"
)({
  beforeLoad: ({ context: { onboarding } }) => leaveIfOnboardingComplete(onboarding),
  loader: async ({ context: { queryClient, onboarding } }) => {
    await queryClient.ensureQueryData(workspaceQuery(onboarding.workspaceId)).catch(guardPageError);
    return onboarding;
  },
  pendingComponent: RoutePending,
  component: TemplatesStep,
});

function TemplatesStep() {
  const onboarding = Route.useLoaderData();
  const { data: me } = useSuspenseQuery(meQuery);
  const { data: workspace } = useSuspenseQuery(workspaceQuery(onboarding.workspaceId));
  return (
    <XMTemplateList
      workspaceId={workspace.id}
      defaultLanguage={resolveDefaultSurveyLanguage({
        workspaceDefaultLanguage: workspaceSurveyConfig(workspace).defaultSurveyLanguage,
        userLocale: me.locale,
      })}
    />
  );
}
