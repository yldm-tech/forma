import { createFileRoute, redirect } from "@tanstack/react-router";

// Integrations moved out of settings in forma2; the old address still leads there.
export const Route = createFileRoute(
  "/_authenticated/workspaces/$workspaceId/settings/workspace/integrations/"
)({
  beforeLoad: ({ params }) => {
    throw redirect({ href: `/workspaces/${params.workspaceId}/integrations`, replace: true });
  },
});
