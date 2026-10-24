import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { RoutePending } from "../../../../../components/shell/route-states";
import { ContactPage } from "../../../../../features/contacts/contact-page";
import { contactActivityQuery, contactQuery } from "../../../../../lib/queries/contacts";
import { guardPageError } from "../../../../../lib/route-guards";
import { workspaceQuery } from "../../../../../lib/queries/workspaces";

// A contact outside the workspace in the address, or no contact at all, is not found (forma2's getContactAuth).
export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/contacts/$contactId")({
  loader: ({ context: { queryClient }, params }) =>
    Promise.all([
      queryClient.ensureQueryData(contactQuery(params.workspaceId, params.contactId)),
      queryClient.ensureQueryData(contactActivityQuery(params.workspaceId, params.contactId)),
    ]).catch(guardPageError),
  pendingComponent: RoutePending,
  component: Contact,
});

function Contact() {
  const { workspaceId, contactId } = Route.useParams();
  const { data: workspace } = useSuspenseQuery(workspaceQuery(workspaceId));
  return (
    <ContactPage workspaceId={workspaceId} contactId={contactId} isReadOnly={workspace.access.isReadOnly} />
  );
}
