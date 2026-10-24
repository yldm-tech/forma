import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { OrganizationGeneralPage } from "../../../../../features/organization/general";
import { organizationQuery } from "../../../../../lib/queries/organizations";

export const Route = createFileRoute("/_authenticated/organizations/$organizationId/settings/general")({
  component: GeneralSettings,
});

function GeneralSettings() {
  const { organizationId } = Route.useParams();
  const { data: organization } = useSuspenseQuery(organizationQuery(organizationId));
  return <OrganizationGeneralPage organization={organization} />;
}
