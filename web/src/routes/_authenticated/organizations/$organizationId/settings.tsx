import { useSuspenseQuery } from "@tanstack/react-query";
import { Outlet, createFileRoute, redirect } from "@tanstack/react-router";
import { RoutePending } from "../../../../components/shell/route-states";
import { SettingsShell } from "../../../../components/shell/settings-shell";
import { guardPageError } from "../../../../lib/route-guards";
import { organizationNavigationQuery, organizationQuery } from "../../../../lib/queries/organizations";

// Organization settings are for members: anyone else goes to the root page, and a billing member, who may read only the billing page, goes where the organization route sends them from every other one (forma2's settings layout and redirectBillingRoleFromRestrictedOrgSettings).
export const Route = createFileRoute("/_authenticated/organizations/$organizationId/settings")({
  loader: async ({ context: { queryClient }, params, location }) => {
    const organization = await queryClient
      .ensureQueryData(organizationQuery(params.organizationId))
      .catch((error: unknown) => guardPageError(error, "home"));
    const isBillingPage = location.pathname.replace(/\/$/, "").endsWith("/settings/billing");
    if (organization.membership.role === "billing" && !isBillingPage) {
      const navigation = await queryClient.fetchQuery(organizationNavigationQuery(params.organizationId));
      throw redirect({ href: navigation.path, replace: true });
    }
  },
  pendingComponent: RoutePending,
  component: OrganizationSettingsLayout,
});

function OrganizationSettingsLayout() {
  const { organizationId } = Route.useParams();
  const { data: organization } = useSuspenseQuery(organizationQuery(organizationId));
  return (
    <SettingsShell organization={organization}>
      <Outlet />
    </SettingsShell>
  );
}
