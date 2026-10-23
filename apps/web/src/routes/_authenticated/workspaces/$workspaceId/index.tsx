import { createFileRoute, redirect } from "@tanstack/react-router";

// forma2's workspace page: the workspace opens on its surveys. The workspace layout's loader, which runs for the surveys page as well, sends a billing member to the billing path instead.
export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/")({
  beforeLoad: ({ params }) => {
    throw redirect({ href: `/workspaces/${params.workspaceId}/surveys`, replace: true });
  },
});
