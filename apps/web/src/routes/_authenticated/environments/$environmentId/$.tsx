import { createFileRoute, redirect } from "@tanstack/react-router";
import { guardPageError } from "../../../../lib/route-guards";
import { environmentQuery } from "../../../../lib/queries/workspaces";

// forma2's /environments/{id}/{path}: the same page of the workspace the id resolves to, after the same access check as the bare address. The pre-rename /environments/{id}/project/{path} first became /environments/{id}/workspace/{path} (forma2's next.config.mjs), and is forwarded from there like any other path.
export const Route = createFileRoute("/_authenticated/environments/$environmentId/$")({
  beforeLoad: async ({ context: { queryClient }, params, location }) => {
    const { workspaceId } = await queryClient
      .fetchQuery(environmentQuery(params.environmentId))
      .catch(guardPageError);
    const path = (params._splat ?? "").replace(/^project(?=\/|$)/, "workspace");
    throw redirect({ href: `/workspaces/${workspaceId}/${path}${location.searchStr}`, replace: true });
  },
});
