import { useSuspenseQuery } from "@tanstack/react-query";
import { Outlet, createFileRoute, redirect } from "@tanstack/react-router";
import { RoutePending } from "../../../components/shell/route-states";
import { SettingsShell } from "../../../components/shell/settings-shell";
import { accountSettingsOrganizationId } from "../../../lib/account-settings";
import { organizationQuery } from "../../../lib/queries/organizations";

// The account settings pages, framed by the settings shell of the organization the user is working in; a user without an organization goes to the root page, which sends them to create one.
export const Route = createFileRoute("/_authenticated/account/settings")({
  loader: async ({ context: { queryClient } }) => {
    const organizationId = await accountSettingsOrganizationId(queryClient);
    if (!organizationId) throw redirect({ to: "/", replace: true });
    await queryClient.ensureQueryData(organizationQuery(organizationId));
    return { organizationId };
  },
  pendingComponent: RoutePending,
  component: AccountSettingsLayout,
});

function AccountSettingsLayout() {
  const { organizationId } = Route.useLoaderData();
  const { data: organization } = useSuspenseQuery(organizationQuery(organizationId));
  return (
    <SettingsShell organization={organization}>
      <Outlet />
    </SettingsShell>
  );
}
