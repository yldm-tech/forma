import { createFileRoute } from "@tanstack/react-router";
import { AuthorizePage } from "../../../features/account/authorize";
import { stringParam } from "../../../features/auth/search";

interface AuthorizeSearch {
  /** The authorization request the page answers. The server also passes client_id, scope and redirect_uri, which forma2's page read; this one shows what the server holds for the request instead. */
  request_id?: string;
}

// The consent page an MCP client's authorization leads to. The layout signs the user in first and brings them back here.
export const Route = createFileRoute("/_authenticated/account/authorize")({
  validateSearch: (search: Record<string, unknown>): AuthorizeSearch => ({
    request_id: stringParam(search.request_id),
  }),
  component: AuthorizeRoute,
});

function AuthorizeRoute() {
  const { request_id: requestId } = Route.useSearch();
  return <AuthorizePage requestId={requestId} />;
}
