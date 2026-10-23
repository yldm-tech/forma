import { createFileRoute, redirect } from "@tanstack/react-router";
import { organizationSettingsPath } from "../../../../../lib/navigation";

export const Route = createFileRoute("/_authenticated/organizations/$organizationId/settings/")({
  beforeLoad: ({ params }) => {
    throw redirect({ href: organizationSettingsPath(params.organizationId, "general"), replace: true });
  },
});
