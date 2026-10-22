import { createFileRoute, redirect } from "@tanstack/react-router";

// The old address of an integration page leads to its new one, keeping the rest of the path.
export const Route = createFileRoute(
  "/_authenticated/workspaces/$workspaceId/settings/workspace/integrations/$"
)({
  beforeLoad: ({ params }) => {
    throw redirect({
      href: `/workspaces/${params.workspaceId}/integrations/${params._splat ?? ""}`,
      replace: true,
    });
  },
});
