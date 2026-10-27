import type { QueryClient } from "@tanstack/react-query";
import { redirect } from "@tanstack/react-router";
import { workspaceQuery } from "../../lib/queries/workspaces";
import { integrationsQuery } from "./api";

/** The ?error a provider's OAuth callback sends the browser back with. */
export interface IntegrationPageSearch {
  error?: string;
}

export const validateIntegrationSearch = (search: Record<string, unknown>): IntegrationPageSearch => ({
  error: typeof search.error === "string" ? search.error : undefined,
});

/** An integration page's data. A member who cannot manage the workspace goes back to the integrations page, as forma2's pages redirected read-only members: connecting an account and choosing which surveys' responses it receives are a manager's decision, which the server enforces. */
export const loadIntegrationPage = async (queryClient: QueryClient, workspaceId: string) => {
  const workspace = await queryClient.ensureQueryData(workspaceQuery(workspaceId));
  if (!workspace.access.canManage) {
    throw redirect({ to: "/workspaces/$workspaceId/integrations", params: { workspaceId }, replace: true });
  }
  await queryClient.ensureQueryData(integrationsQuery(workspaceId));
};
