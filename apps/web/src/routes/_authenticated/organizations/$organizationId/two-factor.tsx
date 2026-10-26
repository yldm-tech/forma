import { createFileRoute } from "@tanstack/react-router";
import { RoutePending } from "../../../../components/shell/route-states";
import { TwoFactorRequiredPage } from "../../../../features/organization/two-factor-required-page";
import { guardPageError } from "../../../../lib/route-guards";
import { organizationQuery } from "../../../../lib/queries/organizations";

// The guide to the two-factor authentication an organization requires of its members. Reading the organization's page is one of the few requests its requirement never refuses (with its navigation and leaving it), so a member it refuses can always reach this page; anyone who is not a member goes to the root page.
export const Route = createFileRoute("/_authenticated/organizations/$organizationId/two-factor")({
  loader: async ({ context: { queryClient }, params }) => {
    await queryClient
      .ensureQueryData(organizationQuery(params.organizationId))
      .catch((error: unknown) => guardPageError(error, "home"));
  },
  pendingComponent: RoutePending,
  component: TwoFactorRequired,
});

function TwoFactorRequired() {
  const { organizationId } = Route.useParams();
  return <TwoFactorRequiredPage organizationId={organizationId} />;
}
