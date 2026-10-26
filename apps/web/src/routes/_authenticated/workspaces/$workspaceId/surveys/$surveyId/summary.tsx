import { ZUserLocale } from "@forma/types/user";
import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { useIsPhone } from "../../../../../../components/shell/no-mobile-overlay";
import { IdBadge } from "../../../../../../components/ui/id-badge";
import { analysisSurveyQuery } from "../../../../../../features/survey-analysis/api";
import { SurveyAnalysisLayout } from "../../../../../../features/survey-analysis/components/analysis-layout";
import { SummaryPage } from "../../../../../../features/survey-analysis/components/summary/summary-page";
import { PHONE_READY } from "../../../../../../lib/phone";
import { meQuery } from "../../../../../../lib/queries/auth";
import { workspaceQuery } from "../../../../../../lib/queries/workspaces";

// forma2's survey summary, /workspaces/{id}/surveys/{surveyId}/summary, where the response notification emails lead. On a phone it opens read-only.
export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/surveys/$surveyId/summary")({
  staticData: PHONE_READY,
  component: SurveySummaryRoute,
});

function SurveySummaryRoute() {
  const { t } = useTranslation();
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
      activeId="summary"
      showTools={!isPhone}>
      <SummaryPage
        survey={survey}
        appSetupCompleted={workspace.appSetupCompleted}
        locale={ZUserLocale.safeParse(me.locale).data ?? "en-US"}
        isReadOnly={workspace.access.isReadOnly || isPhone}
      />
      <IdBadge id={survey.id} label={t("common.survey_id")} />
    </SurveyAnalysisLayout>
  );
}
