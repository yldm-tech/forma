import { createFileRoute, redirect } from "@tanstack/react-router";

// forma2's workspace page: the workspace opens on its surveys. The layout above has already sent a billing member to the billing path.
export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/")({
  beforeLoad: ({ params }) => {
    throw redirect({ href: `/workspaces/${params.workspaceId}/surveys`, replace: true });
  },
});
