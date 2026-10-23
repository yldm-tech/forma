import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { useRouterState } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { LimitsReachedBanner } from "../../features/billing/components/limits-reached-banner";
import { workspaceHomePath } from "../../lib/navigation";
import { meQuery } from "../../lib/queries/auth";
import { billingSummaryQuery } from "../../lib/queries/billing";
import type { OrganizationDetail } from "../../lib/queries/organizations";
import { WORKSPACE_ID_KEY, readStorage } from "../../lib/storage";
import { AppShell } from "./app-shell";
import { SettingsNavigation } from "./settings-navigation";
import { TopControlBar } from "./top-control-bar";

/** forma2's SettingsShell for the organization and account settings pages, which belong to no workspace: the settings sidebar, a top bar with the organization switcher only, and on Forma Cloud the limits banner. */
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
  const { data: billing } = useQuery(billingSummaryQuery(organization.id));
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
          backUrl={rememberedWorkspace ? workspaceHomePath(rememberedWorkspace) : "/"}
          pathname={pathname}
          isFormaCloud={billing?.isFormaCloud}
        />
      }
      topBar={
        <TopControlBar user={me} organization={organization} membershipRole={organization.membership.role} />
      }>
      {children}
    </AppShell>
  );
};
