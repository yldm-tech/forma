import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { ApiKeysPage } from "../../../../../features/organization/api-keys";
import { organizationQuery } from "../../../../../lib/queries/organizations";

export const Route = createFileRoute("/_authenticated/organizations/$organizationId/settings/api-keys")({
  component: ApiKeysSettings,
});

function ApiKeysSettings() {
  const { organizationId } = Route.useParams();
  const { data: organization } = useSuspenseQuery(organizationQuery(organizationId));
  return <ApiKeysPage organization={organization} />;
}
