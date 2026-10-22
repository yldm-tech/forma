import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { TagsPage } from "../../../../features/tags/tags-page";
import { workspaceQuery } from "../../../../lib/queries/workspaces";

export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/tags")({
  component: Tags,
});

function Tags() {
  const { workspaceId } = Route.useParams();
  const { data: workspace } = useSuspenseQuery(workspaceQuery(workspaceId));
  return <TagsPage workspaceId={workspaceId} isReadOnly={workspace.access.isReadOnly} />;
}
