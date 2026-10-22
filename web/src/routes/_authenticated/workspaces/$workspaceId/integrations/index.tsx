import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { IntegrationsPage } from "../../../../../features/workspace-settings/integrations";
import { workspaceQuery } from "../../../../../lib/queries/workspaces";

export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/integrations/")({
  component: Page,
});

function Page() {
  const { workspaceId } = Route.useParams();
  const { data: workspace } = useSuspenseQuery(workspaceQuery(workspaceId));
  return <IntegrationsPage workspace={workspace} />;
}
