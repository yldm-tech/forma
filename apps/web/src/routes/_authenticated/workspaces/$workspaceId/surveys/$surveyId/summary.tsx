import { ZUserLocale } from "@forma/types/user";
import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { IdBadge } from "../../../../../../components/ui/id-badge";
import { analysisSurveyQuery } from "../../../../../../features/survey-analysis/api";
import { SurveyAnalysisLayout } from "../../../../../../features/survey-analysis/components/analysis-layout";
import { SummaryPage } from "../../../../../../features/survey-analysis/components/summary/summary-page";
import { meQuery } from "../../../../../../lib/queries/auth";
import { workspaceQuery } from "../../../../../../lib/queries/workspaces";

// forma2's survey summary, /workspaces/{id}/surveys/{surveyId}/summary.
export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/surveys/$surveyId/summary")({
  component: SurveySummaryRoute,
});

function SurveySummaryRoute() {
  const { t } = useTranslation();
  const { workspaceId, surveyId } = Route.useParams();
  const { data: me } = useSuspenseQuery(meQuery);
  const { data: workspace } = useSuspenseQuery(workspaceQuery(workspaceId));
  const { data: survey } = useSuspenseQuery(analysisSurveyQuery(surveyId));
  return (
    <SurveyAnalysisLayout survey={survey} workspace={workspace} user={me} activeId="summary">
      <SummaryPage
        survey={survey}
        appSetupCompleted={workspace.appSetupCompleted}
        locale={ZUserLocale.safeParse(me.locale).data ?? "en-US"}
        isReadOnly={workspace.access.isReadOnly}
      />
      <IdBadge id={survey.id} label={t("common.survey_id")} />
    </SurveyAnalysisLayout>
  );
}
