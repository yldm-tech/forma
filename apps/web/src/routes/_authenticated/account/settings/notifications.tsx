import { createFileRoute } from "@tanstack/react-router";
import { NotificationsPage } from "../../../../features/account/notifications";
import { notificationsQuery } from "../../../../lib/queries/auth";

interface NotificationsSearch {
  type?: string;
  elementId?: string;
}

export const Route = createFileRoute("/_authenticated/account/settings/notifications")({
  validateSearch: (search: Record<string, unknown>): NotificationsSearch => ({
    type: typeof search.type === "string" ? search.type : undefined,
    elementId: typeof search.elementId === "string" ? search.elementId : undefined,
  }),
  loader: ({ context: { queryClient } }) => queryClient.ensureQueryData(notificationsQuery),
  component: Notifications,
});

function Notifications() {
  const search = Route.useSearch();
  return <NotificationsPage link={search} />;
}
