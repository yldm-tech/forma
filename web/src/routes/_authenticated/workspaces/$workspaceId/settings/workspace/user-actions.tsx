import { createFileRoute, redirect } from "@tanstack/react-router";

// User actions moved out of settings and into the sidebar in forma2; the old address, which the docs and bookmarks carry, still leads there.
export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/settings/workspace/user-actions")({
  beforeLoad: ({ params }) => {
    throw redirect({ href: `/workspaces/${params.workspaceId}/user-actions`, replace: true });
  },
});
