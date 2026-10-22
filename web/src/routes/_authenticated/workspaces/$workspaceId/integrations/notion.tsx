import { createFileRoute } from "@tanstack/react-router";
import { NotionPage } from "../../../../../features/integrations/components/notion";
import {
  loadIntegrationPage,
  validateIntegrationSearch,
} from "../../../../../features/integrations/route-loaders";

export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/integrations/notion")({
  validateSearch: validateIntegrationSearch,
  loader: ({ context: { queryClient }, params }) => loadIntegrationPage(queryClient, params.workspaceId),
  component: Page,
});

function Page() {
  const { workspaceId } = Route.useParams();
  const { error } = Route.useSearch();
  return <NotionPage workspaceId={workspaceId} oauthError={error} />;
}
