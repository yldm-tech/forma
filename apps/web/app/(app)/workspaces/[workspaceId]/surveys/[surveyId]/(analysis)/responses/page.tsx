import { AuthenticationError, ResourceNotFoundError } from "@forma/types/errors";
import { SurveyAnalysisNavigation } from "@/app/(app)/workspaces/[workspaceId]/surveys/[surveyId]/(analysis)/components/SurveyAnalysisNavigation";
import { ResponsePage } from "@/app/(app)/workspaces/[workspaceId]/surveys/[surveyId]/(analysis)/responses/components/ResponsePage";
import { SurveyAnalysisCTA } from "@/app/(app)/workspaces/[workspaceId]/surveys/[surveyId]/(analysis)/summary/components/SurveyAnalysisCTA";
import { getAISmartToolsUnavailableReason, getOrganizationAIConfig } from "@/lib/ai/service";
import { IS_FORMA_CLOUD, IS_STORAGE_CONFIGURED, RESPONSES_PER_PAGE } from "@/lib/constants";
import { getPublicDomain } from "@/lib/getPublicUrl";
import { getResponseCountBySurveyId, getResponses } from "@/lib/response/service";
import { getSurvey } from "@/lib/survey/service";
import { getTagsByWorkspaceId } from "@/lib/tag/service";
import { getUser } from "@/lib/user/service";
import { getTranslate } from "@/lingodotdev/server";
import { getSegments } from "@/modules/contacts/segments/lib/segments";
import { getIsContactsEnabled, getIsQuotasEnabled } from "@/modules/license-check/lib/utils";
import { getQuotas } from "@/modules/quotas/lib/quotas";
import { getSurveyAuth } from "@/modules/survey/lib/survey-auth";
import { PageContentWrapper } from "@/modules/ui/components/page-content-wrapper";
import { PageHeader } from "@/modules/ui/components/page-header";

const Page = async (props: Readonly<{ params: Promise<{ workspaceId: string; surveyId: string }> }>) => {
  const params = await props.params;
  const t = await getTranslate();

  const { session, organization, isReadOnly, workspace } = await getSurveyAuth(
    params.workspaceId,
    params.surveyId
  );

  const [survey, user, tags, isContactsEnabled, responseCount] = await Promise.all([
    getSurvey(params.surveyId),
    getUser(session.user.id),
    getTagsByWorkspaceId(workspace.id),
    getIsContactsEnabled(),
    getResponseCountBySurveyId(params.surveyId),
  ]);

  if (!survey) {
    throw new ResourceNotFoundError(t("common.survey"), params.surveyId);
  }

  if (!user) {
    throw new AuthenticationError(t("common.not_authenticated"));
  }

  if (!organization) {
    throw new ResourceNotFoundError(t("common.organization"), null);
  }

  const segments = isContactsEnabled ? await getSegments(workspace.id) : [];

  const publicDomain = getPublicDomain();

  const isQuotasAllowed = await getIsQuotasEnabled();
  const quotas = isQuotasAllowed ? await getQuotas(survey.id) : [];

  const aiConfig = await getOrganizationAIConfig(organization.id);
  const aiUnavailableReason = getAISmartToolsUnavailableReason(aiConfig) ?? null;

  // Fetch initial responses on the server to prevent duplicate client-side fetch
  const initialResponses = await getResponses(params.surveyId, RESPONSES_PER_PAGE, 0);

  return (
    <PageContentWrapper>
      <PageHeader
        pageTitle={survey.name}
        cta={
          <SurveyAnalysisCTA
            isReadOnly={isReadOnly}
            user={user}
            publicDomain={publicDomain}
            responseCount={responseCount}
            segments={segments}
            isContactsEnabled={isContactsEnabled}
            isFormaCloud={IS_FORMA_CLOUD}
            isStorageConfigured={IS_STORAGE_CONFIGURED}
            aiUnavailableReason={aiUnavailableReason}
          />
        }>
        <SurveyAnalysisNavigation survey={survey} activeId="responses" />
      </PageHeader>
      <ResponsePage
        survey={survey}
        surveyId={params.surveyId}
        environmentTags={tags}
        user={user}
        responsesPerPage={RESPONSES_PER_PAGE}
        locale={user.locale}
        isReadOnly={isReadOnly}
        isQuotasAllowed={isQuotasAllowed}
        quotas={quotas}
        initialResponses={initialResponses}
      />
    </PageContentWrapper>
  );
};

export default Page;
