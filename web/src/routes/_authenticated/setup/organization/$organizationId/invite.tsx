import { createFileRoute, notFound } from "@tanstack/react-router";
import { RoutePending } from "../../../../../components/shell/route-states";
import { SetupLayout } from "../../../../../components/shell/setup-layout";
import { InviteMembers } from "../../../../../features/setup/invite-members";
import { guardPageError } from "../../../../../lib/route-guards";
import { organizationQuery } from "../../../../../lib/queries/organizations";

// Owners only: the onboarding invite always makes the invitee an owner, so a manager gets a not-found rather than a form that fails on submit (forma2's hasSetupInviteAccess).
export const Route = createFileRoute("/_authenticated/setup/organization/$organizationId/invite")({
  loader: async ({ context: { queryClient }, params }) => {
    const organization = await queryClient
      .ensureQueryData(organizationQuery(params.organizationId))
      .catch(guardPageError);
    if (organization.membership.role !== "owner") throw notFound();
  },
  pendingComponent: RoutePending,
  component: SetupInvite,
});

function SetupInvite() {
  const { organizationId } = Route.useParams();
  return (
    <SetupLayout>
      <InviteMembers organizationId={organizationId} />
    </SetupLayout>
  );
}
