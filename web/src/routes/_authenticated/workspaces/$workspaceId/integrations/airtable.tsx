import { createFileRoute } from "@tanstack/react-router";
import { AirtablePage } from "../../../../../features/integrations/components/airtable";
import {
  loadIntegrationPage,
  validateIntegrationSearch,
} from "../../../../../features/integrations/route-loaders";

export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/integrations/airtable")({
  validateSearch: validateIntegrationSearch,
  loader: ({ context: { queryClient }, params }) => loadIntegrationPage(queryClient, params.workspaceId),
  component: Page,
});

function Page() {
  const { workspaceId } = Route.useParams();
  const { error } = Route.useSearch();
  return <AirtablePage workspaceId={workspaceId} oauthError={error} />;
}
