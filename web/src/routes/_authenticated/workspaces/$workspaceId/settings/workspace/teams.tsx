import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { WorkspaceTeamsPage } from "../../../../../../features/workspace-settings/teams";
import { workspaceQuery } from "../../../../../../lib/queries/workspaces";

export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/settings/workspace/teams")({
  component: TeamsSettings,
});

function TeamsSettings() {
  const { workspaceId } = Route.useParams();
  const { data: workspace } = useSuspenseQuery(workspaceQuery(workspaceId));
  return <WorkspaceTeamsPage workspace={workspace} />;
}
