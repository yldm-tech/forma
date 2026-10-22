import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { SegmentsPage } from "../../../../features/segments/segments-page";
import { workspaceQuery } from "../../../../lib/queries/workspaces";

export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/segments")({
  component: Segments,
});

function Segments() {
  const { workspaceId } = Route.useParams();
  const { data: workspace } = useSuspenseQuery(workspaceQuery(workspaceId));
  return <SegmentsPage workspaceId={workspaceId} isReadOnly={workspace.access.isReadOnly} />;
}
