import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { webhooksQuery } from "../../../../../features/integrations/api";
import { WebhooksPage } from "../../../../../features/integrations/components/webhooks";
import { workspaceQuery } from "../../../../../lib/queries/workspaces";

export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/integrations/webhooks")({
  loader: ({ context: { queryClient }, params }) =>
    queryClient.ensureQueryData(webhooksQuery(params.workspaceId)),
  component: Page,
});

function Page() {
  const { workspaceId } = Route.useParams();
  const { data: workspace } = useSuspenseQuery(workspaceQuery(workspaceId));
  return <WebhooksPage workspaceId={workspaceId} isReadOnly={workspace.access.isReadOnly} />;
}
