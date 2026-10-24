import { createFileRoute, redirect } from "@tanstack/react-router";

// Tags moved out of settings in forma2; the old address still leads there.
export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/settings/workspace/tags")({
  beforeLoad: ({ params }) => {
    throw redirect({ href: `/workspaces/${params.workspaceId}/tags`, replace: true });
  },
});
