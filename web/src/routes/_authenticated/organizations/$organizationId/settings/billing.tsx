import { createFileRoute } from "@tanstack/react-router";
import { RoutePending } from "../../../../../components/shell/route-states";
import { stringParam } from "../../../../../features/auth/search";
import { BillingSettingsPage } from "../../../../../features/billing/billing-page";
import type { BillingSearch } from "../../../../../features/billing/components/pricing-table";
import { billingPageQuery } from "../../../../../lib/queries/billing";
import { guardPageError } from "../../../../../lib/route-guards";

// forma2's organization billing settings. Off Forma Cloud the server answers a not-found, which is this page's not-found, as forma2's notFound(). Stripe Checkout returns here with checkout_success (and, for an upgrade, upgrade_pending and session_id).
export const Route = createFileRoute("/_authenticated/organizations/$organizationId/settings/billing")({
  validateSearch: (search: Record<string, unknown>): BillingSearch => ({
    checkout_success: stringParam(search.checkout_success),
    upgrade_pending: stringParam(search.upgrade_pending),
    session_id: stringParam(search.session_id),
  }),
  loader: ({ context: { queryClient }, params }) =>
    queryClient.ensureQueryData(billingPageQuery(params.organizationId)).catch(guardPageError),
  pendingComponent: RoutePending,
  component: BillingSettings,
});

function BillingSettings() {
  const { organizationId } = Route.useParams();
  const search = Route.useSearch();
  return <BillingSettingsPage organizationId={organizationId} search={search} />;
}
