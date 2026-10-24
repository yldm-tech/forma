import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { AppConnectionPage } from "../../../../../features/workspace-settings/app-connection";
import { workspaceQuery } from "../../../../../lib/queries/workspaces";

export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/integrations/app-connection")({
  component: Page,
});

function Page() {
  const { workspaceId } = Route.useParams();
  const { data: workspace } = useSuspenseQuery(workspaceQuery(workspaceId));
  return <AppConnectionPage workspace={workspace} />;
}
