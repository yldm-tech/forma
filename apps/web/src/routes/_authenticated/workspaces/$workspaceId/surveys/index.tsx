import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { useIsPhone } from "../../../../../components/shell/no-mobile-overlay";
import {
  resolveDefaultSurveyLanguage,
  surveyTypeForChannel,
  workspaceSurveyConfig,
} from "../../../../../features/surveys/defaults";
import { SurveysList } from "../../../../../features/surveys/list/survey-list";
import { PHONE_READY } from "../../../../../lib/phone";
import { meQuery } from "../../../../../lib/queries/auth";
import { accessFlags } from "../../../../../lib/roles";
import { workspaceQuery } from "../../../../../lib/queries/workspaces";

// forma2's survey list, /workspaces/{id}/surveys. On a phone it opens read-only: the surveys lead to their results, and creating or changing one needs a bigger screen.
export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/surveys/")({
  staticData: PHONE_READY,
  component: SurveysPage,
});

function SurveysPage() {
  const { t } = useTranslation();
  const { workspaceId } = Route.useParams();
  const { data: me } = useSuspenseQuery(meQuery);
  const { data: workspace } = useSuspenseQuery(workspaceQuery(workspaceId));
  const isPhone = useIsPhone();
  const config = workspaceSurveyConfig(workspace);
  return (
    <SurveysList
      key={workspace.id}
      workspaceId={workspace.id}
      organizationId={workspace.organizationId}
      isReadOnly={workspace.access.isReadOnly || isPhone}
      canSaveAsTemplate={accessFlags(workspace.access.role).isOwnerOrManager && !isPhone}
      readOnlyEmptyText={
        isPhone && !workspace.access.isReadOnly ? t("common.mobile_no_surveys_create_on_desktop") : undefined
      }
      channel={config.channel}
      surveyType={surveyTypeForChannel(config.channel)}
      defaultLanguage={resolveDefaultSurveyLanguage({
        workspaceDefaultLanguage: config.defaultSurveyLanguage,
        userLocale: me.locale,
      })}
    />
  );
}
