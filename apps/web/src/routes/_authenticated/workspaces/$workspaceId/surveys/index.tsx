import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import {
  resolveDefaultSurveyLanguage,
  surveyTypeForChannel,
  workspaceSurveyConfig,
} from "../../../../../features/surveys/defaults";
import { SurveysList } from "../../../../../features/surveys/list/survey-list";
import { meQuery } from "../../../../../lib/queries/auth";
import { accessFlags } from "../../../../../lib/roles";
import { workspaceQuery } from "../../../../../lib/queries/workspaces";

// forma2's survey list, /workspaces/{id}/surveys.
export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/surveys/")({
  component: SurveysPage,
});

function SurveysPage() {
  const { workspaceId } = Route.useParams();
  const { data: me } = useSuspenseQuery(meQuery);
  const { data: workspace } = useSuspenseQuery(workspaceQuery(workspaceId));
  const config = workspaceSurveyConfig(workspace);
  return (
    <SurveysList
      key={workspace.id}
      workspaceId={workspace.id}
      organizationId={workspace.organizationId}
      isReadOnly={workspace.access.isReadOnly}
      canSaveAsTemplate={accessFlags(workspace.access.role).isOwnerOrManager}
      channel={config.channel}
      surveyType={surveyTypeForChannel(config.channel)}
      defaultLanguage={resolveDefaultSurveyLanguage({
        workspaceDefaultLanguage: config.defaultSurveyLanguage,
        userLocale: me.locale,
      })}
    />
  );
}
