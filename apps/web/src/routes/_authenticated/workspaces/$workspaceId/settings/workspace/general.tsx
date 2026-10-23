import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { WorkspaceGeneralPage } from "../../../../../../features/workspace-settings/general";
import { workspaceQuery } from "../../../../../../lib/queries/workspaces";

export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/settings/workspace/general")({
  component: GeneralSettings,
});

function GeneralSettings() {
  const { workspaceId } = Route.useParams();
  const { data: workspace } = useSuspenseQuery(workspaceQuery(workspaceId));
  return <WorkspaceGeneralPage workspace={workspace} />;
}
