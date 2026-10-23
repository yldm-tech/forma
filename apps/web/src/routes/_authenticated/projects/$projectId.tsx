import { createFileRoute, redirect } from "@tanstack/react-router";

// forma2's projects were renamed workspaces; /projects/{id} became /workspaces/{id} (next.config.mjs), the query kept.
export const Route = createFileRoute("/_authenticated/projects/$projectId")({
  beforeLoad: ({ params, location }) => {
    throw redirect({ href: `/workspaces/${params.projectId}${location.searchStr}`, replace: true });
  },
});
