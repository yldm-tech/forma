import { useSuspenseQuery } from "@tanstack/react-query";
import { Outlet, createFileRoute, useRouterState } from "@tanstack/react-router";
import { useEffect } from "react";
import { AppShell } from "../../../components/shell/app-shell";
import { MainNavigation } from "../../../components/shell/main-navigation";
import { RoutePending } from "../../../components/shell/route-states";
import { TopControlBar } from "../../../components/shell/top-control-bar";
import { guardPageError } from "../../../lib/route-guards";
import { meQuery } from "../../../lib/queries/auth";
import { organizationQuery } from "../../../lib/queries/organizations";
import { workspaceQuery } from "../../../lib/queries/workspaces";
import { rememberWorkspace } from "../../../lib/storage";

export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId")({
  loader: async ({ context: { queryClient }, params }) => {
    const workspace = await queryClient
      .ensureQueryData(workspaceQuery(params.workspaceId))
      .catch(guardPageError);
    await queryClient.ensureQueryData(organizationQuery(workspace.organizationId)).catch(guardPageError);
  },
  pendingComponent: RoutePending,
  component: WorkspaceLayout,
});

function WorkspaceLayout() {
  const { workspaceId } = Route.useParams();
  const { data: me } = useSuspenseQuery(meQuery);
  const { data: workspace } = useSuspenseQuery(workspaceQuery(workspaceId));
  const { data: organization } = useSuspenseQuery(organizationQuery(workspace.organizationId));
  const pathname = useRouterState({ select: (state) => state.location.pathname });

  // The root page reopens the workspace the browser used last.
  useEffect(() => rememberWorkspace(workspaceId), [workspaceId]);

  return (
    <AppShell
      sidebar={
        <MainNavigation
          workspaceId={workspace.id}
          organizationId={organization.id}
          membershipRole={organization.membership.role}
          pathname={pathname}
        />
      }
      topBar={
        <TopControlBar
          user={me}
          organization={organization}
          workspace={workspace}
          membershipRole={organization.membership.role}
        />
      }>
      <Outlet />
    </AppShell>
  );
}
