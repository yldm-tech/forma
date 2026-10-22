import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { ContactsPage } from "../../../../../features/contacts/contacts-page";
import { workspaceQuery } from "../../../../../lib/queries/workspaces";

export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/contacts/")({
  component: Contacts,
});

function Contacts() {
  const { workspaceId } = Route.useParams();
  const { data: workspace } = useSuspenseQuery(workspaceQuery(workspaceId));
  return <ContactsPage workspaceId={workspaceId} isReadOnly={workspace.access.isReadOnly} />;
}
