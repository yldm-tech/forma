import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, redirect } from "@tanstack/react-router";
import { RoutePending } from "../../../../../components/shell/route-states";
import {
  resolveDefaultSurveyLanguage,
  surveyTypeForChannel,
  workspaceSurveyConfig,
} from "../../../../../features/surveys/defaults";
import { TemplateGallery } from "../../../../../features/surveys/templates/template-gallery";
import { meQuery } from "../../../../../lib/queries/auth";
import { workspaceQuery } from "../../../../../lib/queries/workspaces";
import { guardPageError } from "../../../../../lib/route-guards";

// forma2's templates page, full screen like the survey editor: a member who may only read the workspace is sent back to its surveys, since every card creates one.
export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId_/surveys/templates")({
  loader: async ({ context: { queryClient }, params }) => {
    const workspace = await queryClient
      .ensureQueryData(workspaceQuery(params.workspaceId))
      .catch(guardPageError);
    if (workspace.access.isReadOnly) {
      throw redirect({ href: `/workspaces/${workspace.id}/surveys`, replace: true });
    }
  },
  pendingComponent: RoutePending,
  component: TemplatesPage,
});

function TemplatesPage() {
  const { workspaceId } = Route.useParams();
  const { data: me } = useSuspenseQuery(meQuery);
  const { data: workspace } = useSuspenseQuery(workspaceQuery(workspaceId));
  const config = workspaceSurveyConfig(workspace);
  return (
    <div className="flex h-screen flex-col">
      <div className="h-full overflow-y-auto bg-slate-50">
        <TemplateGallery
          workspaceId={workspace.id}
          surveyType={surveyTypeForChannel(config.channel)}
          defaultLanguage={resolveDefaultSurveyLanguage({
            workspaceDefaultLanguage: config.defaultSurveyLanguage,
            userLocale: me.locale,
          })}
          isTemplatePage
        />
      </div>
    </div>
  );
}
