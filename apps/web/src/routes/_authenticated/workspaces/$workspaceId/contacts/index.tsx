import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useIsPhone } from "../../../../../components/shell/no-mobile-overlay";
import { ContactsPage } from "../../../../../features/contacts/contacts-page";
import { PHONE_READY } from "../../../../../lib/phone";
import { workspaceQuery } from "../../../../../lib/queries/workspaces";

// The workspace's contacts; on a phone they open read-only, without the attributes and segments tabs, which need a bigger screen.
export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/contacts/")({
  staticData: PHONE_READY,
  component: Contacts,
});

function Contacts() {
  const { workspaceId } = Route.useParams();
  const { data: workspace } = useSuspenseQuery(workspaceQuery(workspaceId));
  const isPhone = useIsPhone();
  return (
    <ContactsPage
      workspaceId={workspaceId}
      isReadOnly={workspace.access.isReadOnly || isPhone}
      showSections={!isPhone}
    />
  );
}
