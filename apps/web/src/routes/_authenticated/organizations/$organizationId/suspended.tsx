import { createFileRoute } from "@tanstack/react-router";
import { RoutePending } from "../../../../components/shell/route-states";
import { MembershipSuspendedPage } from "../../../../features/organization/membership-suspended-page";
import { guardPageError } from "../../../../lib/route-guards";
import { organizationQuery } from "../../../../lib/queries/organizations";

// The notice a member whose access to the organization is suspended lands on. Reading the organization's page is one of the few requests a suspension never refuses (with its navigation and leaving it), so a suspended member can always reach this page; anyone who is not a member goes to the root page.
export const Route = createFileRoute("/_authenticated/organizations/$organizationId/suspended")({
  loader: async ({ context: { queryClient }, params }) => {
    await queryClient
      .ensureQueryData(organizationQuery(params.organizationId))
      .catch((error: unknown) => guardPageError(error, "home"));
  },
  pendingComponent: RoutePending,
  component: MembershipSuspended,
});

function MembershipSuspended() {
  const { organizationId } = Route.useParams();
  return <MembershipSuspendedPage organizationId={organizationId} />;
}
