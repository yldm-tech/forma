import { ZUserLocale } from "@forma/types/user";
import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useIsPhone } from "../../../../../../components/shell/no-mobile-overlay";
import { analysisSurveyQuery } from "../../../../../../features/survey-analysis/api";
import { SurveyAnalysisLayout } from "../../../../../../features/survey-analysis/components/analysis-layout";
import { ResponsePage } from "../../../../../../features/survey-analysis/components/responses/response-page";
import { PHONE_READY } from "../../../../../../lib/phone";
import { meQuery } from "../../../../../../lib/queries/auth";
import { workspaceQuery } from "../../../../../../lib/queries/workspaces";

// forma2's survey responses, /workspaces/{id}/surveys/{surveyId}/responses. On a phone it opens read-only, the responses stacked as cards instead of the table.
export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/surveys/$surveyId/responses")({
  staticData: PHONE_READY,
  component: SurveyResponsesRoute,
});

function SurveyResponsesRoute() {
  const { workspaceId, surveyId } = Route.useParams();
  const { data: me } = useSuspenseQuery(meQuery);
  const { data: workspace } = useSuspenseQuery(workspaceQuery(workspaceId));
  const { data: survey } = useSuspenseQuery(analysisSurveyQuery(surveyId));
  const isPhone = useIsPhone();
  return (
    <SurveyAnalysisLayout
      survey={survey}
      workspace={workspace}
      user={me}
      activeId="responses"
      showTools={!isPhone}>
      <ResponsePage
        survey={survey}
        locale={ZUserLocale.safeParse(me.locale).data ?? "en-US"}
        isReadOnly={workspace.access.isReadOnly || isPhone}
        layout={isPhone ? "cards" : "table"}
      />
    </SurveyAnalysisLayout>
  );
}
