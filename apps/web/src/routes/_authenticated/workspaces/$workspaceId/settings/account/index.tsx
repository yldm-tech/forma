import { createFileRoute, redirect } from "@tanstack/react-router";

// Account settings moved out of the workspace in forma2; its old address opens the profile (next.config.mjs), the query kept.
export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/settings/account/")({
  beforeLoad: ({ location }) => {
    throw redirect({ href: `/account/settings/profile${location.searchStr}`, replace: true });
  },
});
