import { useQuery } from "@tanstack/react-query";
import { useRouterState } from "@tanstack/react-router";
import { MenuIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "../../lib/cn";
import type { Me } from "../../lib/queries/auth";
import { billingSummaryQuery } from "../../lib/queries/billing";
import { Breadcrumb, BreadcrumbList } from "../ui/breadcrumb";
import { Button } from "../ui/button";
import { Sheet } from "../ui/sheet";
import { CommandPalette } from "./command-palette";
import { MobileNavigation } from "./main-navigation";
import { useIsPhone } from "./no-mobile-overlay";
import { OrganizationBreadcrumb } from "./organization-breadcrumb";
import { UserDropdown } from "./user-dropdown";
import { WorkspaceBreadcrumb } from "./workspace-breadcrumb";

export interface ShellOrganization {
  id: string;
  name: string;
}

/**
 * forma2's TopControlBar: the organization and (inside a workspace) workspace switchers on the left; on the right the command palette, scoped to the current workspace, and the user menu.
 *
 * Inside a workspace on a phone-sized screen, where the sidebar is hidden, the bar starts with a menu button that opens the workspace's navigation in a drawer from the left (MobileNavigation), and keeps only the workspace switcher and the user menu: the organization switcher and the palette lead to pages a phone does not open. The drawer closes when a page is chosen in it or the address changes otherwise (a workspace picked in the switcher).
 */
export const TopControlBar = ({
  user,
  organization,
  workspace,
  membershipRole,
  showWorkspaceBreadcrumb = true,
}: {
  user: Me;
  organization: ShellOrganization;
  /** The workspace the palette's destinations belong to. Absent for a user with no workspace in the organization. */
  workspace?: { id: string; name: string };
  membershipRole?: string;
  /** False on the organization and account settings pages, whose address names no workspace: the palette still uses the current one, but the top bar does not claim it. */
  showWorkspaceBreadcrumb?: boolean;
}) => {
  const { t } = useTranslation();
  const isOwnerOrManager = membershipRole === "owner" || membershipRole === "manager";
  const { data: billing } = useQuery(billingSummaryQuery(organization.id));
  const breadcrumbWorkspace = showWorkspaceBreadcrumb ? workspace : undefined;
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  // The drawer is open on the page it was opened on, while the screen is phone-sized: any navigation closes it, and so does widening the window past the sm breakpoint, where the sidebar is back.
  const isPhone = useIsPhone();
  const [drawerOpenedAt, setDrawerOpenedAt] = useState<string | null>(null);
  const isDrawerOpen = isPhone && drawerOpenedAt === pathname;
  const closeDrawer = () => setDrawerOpenedAt(null);
  return (
    <div className="flex h-14 w-full items-center justify-between gap-2 bg-slate-50 px-3 sm:px-6">
      {breadcrumbWorkspace && (
        <>
          <Button
            variant="ghost"
            size="icon"
            className="shrink-0 text-slate-600 sm:hidden"
            aria-label={t("common.mobile_open_navigation")}
            aria-haspopup="dialog"
            aria-expanded={isDrawerOpen}
            onClick={() => setDrawerOpenedAt(pathname)}>
            <MenuIcon strokeWidth={1.5} />
          </Button>
          <Sheet
            side="left"
            open={isDrawerOpen}
            onOpenChange={(open) => setDrawerOpenedAt(open ? pathname : null)}
            title={t("common.mobile_navigation_title")}
            className="max-w-xs">
            <MobileNavigation
              workspaceId={breadcrumbWorkspace.id}
              membershipRole={membershipRole}
              pathname={pathname}
              onNavigate={closeDrawer}
            />
          </Sheet>
        </>
      )}
      <Breadcrumb className="min-w-0 flex-1">
        <BreadcrumbList className="gap-0">
          <OrganizationBreadcrumb
            organizationId={organization.id}
            organizationName={organization.name}
            isLastCrumb={!breadcrumbWorkspace}
            className={breadcrumbWorkspace ? "hidden sm:inline-flex" : undefined}
          />
          {breadcrumbWorkspace && (
            <WorkspaceBreadcrumb
              workspaceId={breadcrumbWorkspace.id}
              workspaceName={breadcrumbWorkspace.name}
              organizationId={organization.id}
              isOwnerOrManager={isOwnerOrManager}
              isMembershipPending={membershipRole === undefined}
            />
          )}
        </BreadcrumbList>
      </Breadcrumb>
      <div className="flex shrink-0 items-center gap-3">
        {/* Everywhere in the product, one keystroke away: the sidebar shows only the top areas, and this reaches the rest. */}
        {workspace && (
          <div className={cn(breadcrumbWorkspace && "hidden sm:contents")}>
            <CommandPalette
              workspaceId={workspace.id}
              organizationId={organization.id}
              isBilling={membershipRole === "billing"}
              isOwnerOrManager={isOwnerOrManager}
              isFormaCloud={billing?.isFormaCloud ?? false}
            />
          </div>
        )}
        <UserDropdown user={user} organizationId={organization.id} isBilling={membershipRole === "billing"} />
      </div>
    </div>
  );
};
