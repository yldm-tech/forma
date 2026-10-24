import { createFileRoute, redirect } from "@tanstack/react-router";

// The old address of an account settings page leads to the same page under /account/settings (forma2's next.config.mjs), the query kept.
export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/settings/account/$")({
  beforeLoad: ({ params, location }) => {
    throw redirect({ href: `/account/settings/${params._splat ?? ""}${location.searchStr}`, replace: true });
  },
});
