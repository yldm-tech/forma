import { createFileRoute } from "@tanstack/react-router";
import { SlackPage } from "../../../../../features/integrations/components/slack";
import {
  loadIntegrationPage,
  validateIntegrationSearch,
} from "../../../../../features/integrations/route-loaders";

export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/integrations/slack")({
  validateSearch: validateIntegrationSearch,
  loader: ({ context: { queryClient }, params }) => loadIntegrationPage(queryClient, params.workspaceId),
  component: Page,
});

function Page() {
  const { workspaceId } = Route.useParams();
  const { error } = Route.useSearch();
  return <SlackPage workspaceId={workspaceId} oauthError={error} />;
}
