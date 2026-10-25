import type { TSurvey } from "@forma/types/surveys/types";
import { useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { PageContentWrapper, PageHeader } from "../../../components/ui/page";
import { SecondaryNavigation } from "../../../components/ui/secondary-navigation";
import type { Me } from "../../../lib/queries/auth";
import type { WorkspaceDetail } from "../../../lib/queries/workspaces";
import { publicDomainQuery } from "../../surveys/api";
import { surveyResponseCountQuery } from "../api";
import { NewResponsesNotice } from "./new-responses-notice";
import { SurveyAnalysisCTA } from "./survey-analysis-cta";

/** The frame of a survey's summary and responses pages: the survey's name, its tools, the two tabs (forma2's PageHeader with SurveyAnalysisCTA and SurveyAnalysisNavigation), and the notice of responses that arrived since the page was loaded. */
export const SurveyAnalysisLayout = ({
  survey,
  workspace,
  user,
  activeId,
  children,
}: {
  survey: TSurvey;
  workspace: WorkspaceDetail;
  user: Me;
  activeId: "summary" | "responses";
  children: ReactNode;
}) => {
  const { t } = useTranslation();
  const base = `/workspaces/${workspace.id}/surveys/${survey.id}`;
  const publicDomain = useQuery(publicDomainQuery);
  // The survey's unfiltered response count: the Responses tab shows it, the tools read it for the edit caution, the reset confirmation and the example responses, and the new-responses notice counts from it.
  const responseCount = useQuery(surveyResponseCountQuery(survey.id, {}));

  return (
    <PageContentWrapper>
      <PageHeader
        pageTitle={survey.name}
        cta={
          publicDomain.data !== undefined && (
            <SurveyAnalysisCTA
              survey={survey}
              workspace={workspace}
              user={user}
              publicDomain={publicDomain.data}
              responseCount={responseCount.data}
            />
          )
        }>
        <SecondaryNavigation
          ariaLabel={survey.name}
          activeId={activeId}
          navigation={[
            { id: "summary", label: t("common.summary"), href: `${base}/summary` },
            {
              id: "responses",
              // The unfiltered total, from the count the tools read already.
              label:
                responseCount.data === undefined
                  ? t("common.responses")
                  : t("workspace.surveys.responses.responses_tab_label", { count: responseCount.data }),
              href: `${base}/responses`,
            },
          ]}
        />
      </PageHeader>
      <NewResponsesNotice survey={survey} loadedCount={responseCount.data} />
      {children}
    </PageContentWrapper>
  );
};
