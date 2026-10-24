import { createFileRoute } from "@tanstack/react-router";
import { GoogleSheetsPage } from "../../../../../features/integrations/components/google-sheets";
import {
  loadIntegrationPage,
  validateIntegrationSearch,
} from "../../../../../features/integrations/route-loaders";

export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/integrations/google-sheets")({
  validateSearch: validateIntegrationSearch,
  loader: ({ context: { queryClient }, params }) => loadIntegrationPage(queryClient, params.workspaceId),
  component: Page,
});

function Page() {
  const { workspaceId } = Route.useParams();
  const { error } = Route.useSearch();
  return <GoogleSheetsPage workspaceId={workspaceId} oauthError={error} />;
}
