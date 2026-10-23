import { createFileRoute } from "@tanstack/react-router";
import { stringParam } from "../../features/auth/search";
import { BillingConfirmation } from "../../features/billing/components/billing-confirmation";

interface BillingConfirmationSearch {
  /** The organization that subscribed; Stripe's success URL carries it. */
  organizationId?: string;
  /** The plan bought, whose arrival the page waits for. */
  plan?: string;
}

// forma2's /billing-confirmation, the success URL of a subscription Checkout.
export const Route = createFileRoute("/_authenticated/billing-confirmation")({
  validateSearch: (search: Record<string, unknown>): BillingConfirmationSearch => ({
    organizationId: stringParam(search.organizationId),
    plan: stringParam(search.plan),
  }),
  component: BillingConfirmationRoute,
});

function BillingConfirmationRoute() {
  const { organizationId, plan } = Route.useSearch();
  return <BillingConfirmation organizationId={organizationId} plan={plan} />;
}
