import { notFound } from "next/navigation";
import { AuthenticationError, ResourceNotFoundError } from "@forma/types/errors";
import { SurveyAnalysisNavigation } from "@/app/(app)/workspaces/[workspaceId]/surveys/[surveyId]/(analysis)/components/SurveyAnalysisNavigation";
import { SummaryPage } from "@/app/(app)/workspaces/[workspaceId]/surveys/[surveyId]/(analysis)/summary/components/SummaryPage";
import { SurveyAnalysisCTA } from "@/app/(app)/workspaces/[workspaceId]/surveys/[surveyId]/(analysis)/summary/components/SurveyAnalysisCTA";
import { getSurveySummary } from "@/app/(app)/workspaces/[workspaceId]/surveys/[surveyId]/(analysis)/summary/lib/surveySummary";
import { getAISmartToolsUnavailableReason, getOrganizationAIConfig } from "@/lib/ai/service";
import { DEFAULT_LOCALE, IS_FORMA_CLOUD, IS_STORAGE_CONFIGURED } from "@/lib/constants";
import { getPublicDomain } from "@/lib/getPublicUrl";
import { getSurvey } from "@/lib/survey/service";
import { getUser } from "@/lib/user/service";
import { getTranslate } from "@/lingodotdev/server";
import { getSegments } from "@/modules/contacts/segments/lib/segments";
import { getIsContactsEnabled, getIsQuotasEnabled } from "@/modules/license-check/lib/utils";
import { getSurveyAuth } from "@/modules/survey/lib/survey-auth";
import { IdBadge } from "@/modules/ui/components/id-badge";
import { PageContentWrapper } from "@/modules/ui/components/page-content-wrapper";
import { PageHeader } from "@/modules/ui/components/page-header";

const SurveyPage = async (
  props: Readonly<{ params: Promise<{ workspaceId: string; surveyId: string }> }>
) => {
  const params = await props.params;
  const t = await getTranslate();

  const surveyId = params.surveyId;

  if (!surveyId) {
    return notFound();
  }

  const { session, isReadOnly, workspace, organization } = await getSurveyAuth(params.workspaceId, surveyId);

  if (!organization) {
    throw new ResourceNotFoundError(t("common.organization"), null);
  }

  // Neither of these touches the database, so resolving them ahead of the reads costs no round trip
  // and keeps the segment read conditional on the same flag it has always been conditional on.
  const [isContactsEnabled, isQuotasAllowed] = await Promise.all([
    getIsContactsEnabled(),
    getIsQuotasEnabled(),
  ]);

  // One stage instead of five: nothing below depends on anything else below, and the summary read
  // is the slow one, so starting it first rather than last is where the latency goes.
  const [survey, user, segments, aiConfig, initialSurveySummary] = await Promise.all([
    getSurvey(params.surveyId),
    getUser(session.user.id),
    isContactsEnabled ? getSegments(workspace.id) : Promise.resolve([]),
    getOrganizationAIConfig(organization.id),
    // Fetched on the server to prevent duplicate API calls during hydration
    getSurveySummary(surveyId),
  ]);

  if (!survey) {
    throw new ResourceNotFoundError(t("common.survey"), params.surveyId);
  }

  if (!user) {
    throw new AuthenticationError(t("common.not_authenticated"));
  }

  const aiUnavailableReason = getAISmartToolsUnavailableReason(aiConfig) ?? null;

  const publicDomain = getPublicDomain();

  return (
    <PageContentWrapper>
      <PageHeader
        pageTitle={survey.name}
        cta={
          <SurveyAnalysisCTA
            isReadOnly={isReadOnly}
            user={user}
            publicDomain={publicDomain}
            responseCount={initialSurveySummary?.meta.totalResponses ?? 0}
            segments={segments}
            isContactsEnabled={isContactsEnabled}
            isFormaCloud={IS_FORMA_CLOUD}
            isStorageConfigured={IS_STORAGE_CONFIGURED}
            aiUnavailableReason={aiUnavailableReason}
          />
        }>
        <SurveyAnalysisNavigation survey={survey} activeId="summary" />
      </PageHeader>
      <SummaryPage
        survey={survey}
        surveyId={params.surveyId}
        locale={user.locale ?? DEFAULT_LOCALE}
        initialSurveySummary={initialSurveySummary}
        isQuotasAllowed={isQuotasAllowed}
        isReadOnly={isReadOnly}
      />

      <IdBadge id={surveyId} label={t("common.survey_id")} variant="column" />
    </PageContentWrapper>
  );
};

export default SurveyPage;
