import { createFileRoute, redirect } from "@tanstack/react-router";
import { isPhoneScreen } from "../../../../components/shell/no-mobile-overlay";
import { NotificationsPage } from "../../../../features/account/notifications";
import { isUnsubscribeLink } from "../../../../lib/notifications";
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
  // The settings are covered on a phone, so an email's unsubscribe link opened there goes to the page that unsubscribes and says so, which a phone may open. It runs before the settings layout loads anything.
  beforeLoad: ({ search }) => {
    if (isUnsubscribeLink(search) && isPhoneScreen()) {
      throw redirect({
        to: "/account/unsubscribe",
        search: { type: search.type, elementId: search.elementId },
        replace: true,
      });
    }
  },
  loader: ({ context: { queryClient } }) => queryClient.ensureQueryData(notificationsQuery),
  component: Notifications,
});

function Notifications() {
  const search = Route.useSearch();
  return <NotificationsPage link={search} />;
}
