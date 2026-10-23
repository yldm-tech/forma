import { createFileRoute, redirect } from "@tanstack/react-router";

// forma2's workspace onboarding under its pre-rename address, /organizations/{id}/projects/new/{path} (next.config.mjs): the same step under workspaces/new, the query kept.
export const Route = createFileRoute("/_authenticated/organizations/$organizationId/projects/new/$")({
  beforeLoad: ({ params, location }) => {
    const path = params._splat ? `/${params._splat}` : "";
    throw redirect({
      href: `/organizations/${params.organizationId}/workspaces/new${path}${location.searchStr}`,
      replace: true,
    });
  },
});
