import { createFileRoute, redirect } from "@tanstack/react-router";

// The old address of an integration page leads to its new one, keeping the rest of the path and the query: the Airtable, Notion and Slack OAuth callbacks send the browser here, with ?error when the provider refused.
export const Route = createFileRoute(
  "/_authenticated/workspaces/$workspaceId/settings/workspace/integrations/$"
)({
  beforeLoad: ({ params, location }) => {
    throw redirect({
      href: `/workspaces/${params.workspaceId}/integrations/${params._splat ?? ""}${location.searchStr}`,
      replace: true,
    });
  },
});
