import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { AttributesPage } from "../../../../features/contacts/attributes-page";
import { workspaceQuery } from "../../../../lib/queries/workspaces";

export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/attributes")({
  component: Attributes,
});

function Attributes() {
  const { workspaceId } = Route.useParams();
  const { data: workspace } = useSuspenseQuery(workspaceQuery(workspaceId));
  return <AttributesPage workspaceId={workspaceId} isReadOnly={workspace.access.isReadOnly} />;
}
