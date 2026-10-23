import { createFileRoute, redirect } from "@tanstack/react-router";
import { workspaceSettingsPath } from "../../../../../../lib/navigation";

export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/settings/workspace/")({
  beforeLoad: ({ params }) => {
    throw redirect({ href: workspaceSettingsPath(params.workspaceId, "general"), replace: true });
  },
});
