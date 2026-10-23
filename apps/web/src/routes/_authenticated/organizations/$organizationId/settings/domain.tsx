import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { RoutePending } from "../../../../../components/shell/route-states";
import { prettyUrlsQuery } from "../../../../../features/organization/whitelabel/api";
import { DomainSettingsPage } from "../../../../../features/organization/whitelabel/domain-page";
import { organizationQuery } from "../../../../../lib/queries/organizations";
import { guardPageError } from "../../../../../lib/route-guards";

// forma2's organization domain settings. On Forma Cloud the server answers a not-found, which is this page's not-found, as forma2's notFound().
export const Route = createFileRoute("/_authenticated/organizations/$organizationId/settings/domain")({
  loader: ({ context: { queryClient }, params }) =>
    queryClient.ensureQueryData(prettyUrlsQuery(params.organizationId)).catch(guardPageError),
  pendingComponent: RoutePending,
  component: DomainSettings,
});

function DomainSettings() {
  const { organizationId } = Route.useParams();
  const { data: organization } = useSuspenseQuery(organizationQuery(organizationId));
  const { data: surveys } = useSuspenseQuery(prettyUrlsQuery(organizationId));
  return <DomainSettingsPage organization={organization} surveys={surveys} />;
}
