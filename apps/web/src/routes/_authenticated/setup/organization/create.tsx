import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { SetupLayout } from "../../../../components/shell/setup-layout";
import { CreateOrganization } from "../../../../features/setup/create-organization";
import { InvitationRequired } from "../../../../features/setup/invitation-required";
import { organizationCreationQuery } from "../../../../lib/queries/organizations";

// forma2's create page with its license gates passing (multiple organizations are always enabled), for whoever may create an organization: anyone where public sign-up is open. On an invite-only instance only the owner of an organization may, so a user who reaches this page without one (an SSO sign-in with no default organization, or someone removed from theirs) is told to ask for an invitation instead.
export const Route = createFileRoute("/_authenticated/setup/organization/create")({
  loader: ({ context: { queryClient } }) => queryClient.ensureQueryData(organizationCreationQuery),
  component: CreateOrganizationPage,
});

function CreateOrganizationPage() {
  const { data: canCreate } = useSuspenseQuery(organizationCreationQuery);
  return <SetupLayout>{canCreate ? <CreateOrganization /> : <InvitationRequired />}</SetupLayout>;
}
