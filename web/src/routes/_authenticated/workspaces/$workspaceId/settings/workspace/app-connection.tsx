import { createFileRoute, redirect } from "@tanstack/react-router";

// The SDK connection is a page inside Integrations in forma2; its old settings address, which the docs and bookmarks carry, still leads there.
export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/settings/workspace/app-connection")({
  beforeLoad: ({ params }) => {
    throw redirect({ href: `/workspaces/${params.workspaceId}/integrations/app-connection`, replace: true });
  },
});
