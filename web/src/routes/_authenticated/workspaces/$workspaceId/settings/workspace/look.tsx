import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { WorkspaceLookPage } from "../../../../../../features/workspace-settings/look";
import { workspaceQuery } from "../../../../../../lib/queries/workspaces";

export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/settings/workspace/look")({
  component: LookSettings,
});

function LookSettings() {
  const { workspaceId } = Route.useParams();
  const { data: workspace } = useSuspenseQuery(workspaceQuery(workspaceId));
  return <WorkspaceLookPage workspace={workspace} />;
}
