import { createFileRoute, redirect } from "@tanstack/react-router";
import { workspaceHomePath } from "../../../lib/navigation";
import { guardPageError } from "../../../lib/route-guards";
import { environmentQuery } from "../../../lib/queries/workspaces";

// forma2's links and bookmarks into /environments/{id} still arrive; the id is a workspace id or a pre-rename environment id.
export const Route = createFileRoute("/_authenticated/environments/$environmentId")({
  beforeLoad: async ({ context: { queryClient }, params }) => {
    const { workspaceId } = await queryClient
      .fetchQuery(environmentQuery(params.environmentId))
      .catch(guardPageError);
    throw redirect({ href: workspaceHomePath(workspaceId), replace: true });
  },
});
