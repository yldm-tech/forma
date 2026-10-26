import { createFileRoute } from "@tanstack/react-router";
import { RoutePending } from "../../../components/shell/route-states";
import { stringParam } from "../../../features/auth/search";
import { UnsubscribePage } from "../../../features/account/unsubscribe";
import { PHONE_READY } from "../../../lib/phone";
import { notificationsQuery } from "../../../lib/queries/auth";

interface UnsubscribeSearch {
  /** The kind of switch: alert, unsubscribedOrganizationIds or unsubscribedWeeklySummaryOrganizationIds. */
  type?: string;
  /** The survey or organization the switch is for. */
  elementId?: string;
}

// Where an email's unsubscribe link lands on a phone (the notification settings send it here): it turns the switch off and says so, outside the settings shell, and opens on a phone.
export const Route = createFileRoute("/_authenticated/account/unsubscribe")({
  staticData: PHONE_READY,
  validateSearch: (search: Record<string, unknown>): UnsubscribeSearch => ({
    type: stringParam(search.type),
    elementId: stringParam(search.elementId),
  }),
  loader: ({ context: { queryClient } }) => queryClient.ensureQueryData(notificationsQuery),
  pendingComponent: RoutePending,
  component: Unsubscribe,
});

function Unsubscribe() {
  const search = Route.useSearch();
  return <UnsubscribePage link={search} />;
}
