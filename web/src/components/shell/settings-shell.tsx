import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { useRouterState } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { LimitsReachedBanner } from "../../features/billing/components/limits-reached-banner";
import { meQuery } from "../../lib/queries/auth";
import { billingSummaryQuery } from "../../lib/queries/billing";
import type { OrganizationDetail } from "../../lib/queries/organizations";
import { workspacesQuery } from "../../lib/queries/workspaces";
import { WORKSPACE_ID_KEY, readStorage } from "../../lib/storage";
import { AppShell } from "./app-shell";
import { SettingsNavigation } from "./settings-navigation";
import { TopControlBar } from "./top-control-bar";

/**
 * The workspace the settings shell shows, forma2's getSettingsLayoutData: the one this browser last opened while it is among the organization's workspaces the user can open, otherwise the first of them; none when there are none.
 */
export const currentSettingsWorkspace = <T extends { id: string }>(
  workspaces: T[] | undefined,
  rememberedId: string | null
): T | undefined => workspaces?.find((workspace) => workspace.id === rememberedId) ?? workspaces?.[0];

/** forma2's SettingsShell for the organization and account settings pages, whose address names no workspace: the settings sidebar with its workspace section for the current workspace, a top bar with the organization switcher and the command palette, and on Forma Cloud the limits banner. */
export const SettingsShell = ({
  organization,
  children,
}: {
  organization: OrganizationDetail;
  children: ReactNode;
}) => {
  const { data: me } = useSuspenseQuery(meQuery);
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const { data: billing } = useQuery(billingSummaryQuery(organization.id));
  const { data: workspaces } = useQuery(workspacesQuery(organization.id));
  const current = currentSettingsWorkspace(workspaces, readStorage(WORKSPACE_ID_KEY));
  const workspace = current && { id: current.id, name: current.name };
  return (
    <AppShell
      banner={
        billing?.isFormaCloud &&
        billing.usage && (
          <LimitsReachedBanner
            organizationId={organization.id}
            limits={billing.limits}
            responseCount={billing.usage.responses}
          />
        )
      }
      sidebar={
        <SettingsNavigation
          organizationId={organization.id}
          organizationName={organization.name}
          membershipRole={organization.membership.role}
          workspace={workspace}
          backUrl={workspace ? `/workspaces/${workspace.id}/surveys` : "/"}
          pathname={pathname}
          isFormaCloud={billing?.isFormaCloud}
        />
      }
      topBar={
        <TopControlBar
          user={me}
          organization={organization}
          workspace={workspace}
          membershipRole={organization.membership.role}
          showWorkspaceBreadcrumb={false}
        />
      }>
      {children}
    </AppShell>
  );
};
