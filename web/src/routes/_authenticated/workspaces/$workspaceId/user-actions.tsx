import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { UserActionsPage } from "../../../../features/actions/user-actions-page";
import { workspaceQuery } from "../../../../lib/queries/workspaces";

export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/user-actions")({
  component: UserActions,
});

function UserActions() {
  const { workspaceId } = Route.useParams();
  const { data: workspace } = useSuspenseQuery(workspaceQuery(workspaceId));
  return <UserActionsPage workspaceId={workspaceId} isReadOnly={workspace.access.isReadOnly} />;
}
