import { useSuspenseQuery } from "@tanstack/react-query";
import { useRouterState } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { workspaceHomePath } from "../../lib/navigation";
import { meQuery } from "../../lib/queries/auth";
import type { OrganizationDetail } from "../../lib/queries/organizations";
import { WORKSPACE_ID_KEY, readStorage } from "../../lib/storage";
import { AppShell } from "./app-shell";
import { SettingsNavigation } from "./settings-navigation";
import { TopControlBar } from "./top-control-bar";

/** forma2's SettingsShell for the organization and account settings pages, which belong to no workspace: the settings sidebar, and a top bar with the organization switcher only. */
export const SettingsShell = ({
  organization,
  children,
}: {
  organization: OrganizationDetail;
  children: ReactNode;
}) => {
  const { data: me } = useSuspenseQuery(meQuery);
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const rememberedWorkspace = readStorage(WORKSPACE_ID_KEY);
  return (
    <AppShell
      sidebar={
        <SettingsNavigation
          organizationId={organization.id}
          organizationName={organization.name}
          membershipRole={organization.membership.role}
          backUrl={rememberedWorkspace ? workspaceHomePath(rememberedWorkspace) : "/"}
          pathname={pathname}
        />
      }
      topBar={
        <TopControlBar user={me} organization={organization} membershipRole={organization.membership.role} />
      }>
      {children}
    </AppShell>
  );
};
