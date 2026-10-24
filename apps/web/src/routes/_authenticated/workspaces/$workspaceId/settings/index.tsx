import { createFileRoute, redirect } from "@tanstack/react-router";
import { workspaceSettingsPath } from "../../../../../lib/navigation";

// forma2's /settings opens the workspace's general settings.
export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/settings/")({
  beforeLoad: ({ params }) => {
    throw redirect({ href: workspaceSettingsPath(params.workspaceId, "general"), replace: true });
  },
});
