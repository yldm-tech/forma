import { createFileRoute, redirect } from "@tanstack/react-router";
import { guardPageError } from "../../../../../../lib/route-guards";
import { workspaceQuery } from "../../../../../../lib/queries/workspaces";

// forma2's back-compat shim for the organization settings it once kept under a workspace, /workspaces/{id}/settings/organization/{path}: bookmarks and Stripe cancel URLs created before the move land on the same page under /organizations/{organizationId}/settings. The workspace is resolved for someone who may open it; anyone else gets the not-found page, so the redirect never reveals the organization.
export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/settings/organization/$")({
  beforeLoad: async ({ context: { queryClient }, params, location }) => {
    const workspace = await queryClient
      .fetchQuery(workspaceQuery(params.workspaceId))
      .catch(guardPageError);
    throw redirect({
      href: `/organizations/${workspace.organizationId}/settings/${params._splat ?? ""}${location.searchStr}`,
      replace: true,
    });
  },
});
