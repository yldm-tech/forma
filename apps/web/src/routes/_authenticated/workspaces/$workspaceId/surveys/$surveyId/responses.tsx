import { ZUserLocale } from "@forma/types/user";
import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { analysisSurveyQuery } from "../../../../../../features/survey-analysis/api";
import { SurveyAnalysisLayout } from "../../../../../../features/survey-analysis/components/analysis-layout";
import { ResponsePage } from "../../../../../../features/survey-analysis/components/responses/response-page";
import { meQuery } from "../../../../../../lib/queries/auth";
import { workspaceQuery } from "../../../../../../lib/queries/workspaces";

// forma2's survey responses, /workspaces/{id}/surveys/{surveyId}/responses.
export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/surveys/$surveyId/responses")({
  component: SurveyResponsesRoute,
});

function SurveyResponsesRoute() {
  const { workspaceId, surveyId } = Route.useParams();
  const { data: me } = useSuspenseQuery(meQuery);
  const { data: workspace } = useSuspenseQuery(workspaceQuery(workspaceId));
  const { data: survey } = useSuspenseQuery(analysisSurveyQuery(surveyId));
  return (
    <SurveyAnalysisLayout survey={survey} workspace={workspace} user={me} activeId="responses">
      <ResponsePage
        survey={survey}
        locale={ZUserLocale.safeParse(me.locale).data ?? "en-US"}
        isReadOnly={workspace.access.isReadOnly}
      />
    </SurveyAnalysisLayout>
  );
}
