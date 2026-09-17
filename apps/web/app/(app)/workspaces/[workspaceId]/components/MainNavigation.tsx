"use client";

import { MessageCircle, SettingsIcon, UserIcon, WorkflowIcon } from "lucide-react";
import { usePathname, useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useState, useTransition } from "react";
import { useTranslation } from "react-i18next";
import { TOrganizationRole } from "@forma/types/memberships";
import { TOrganization } from "@forma/types/organizations";
import {
  getOrganizationsForSwitcherAction,
  getWorkspacesForSwitcherAction,
} from "@/app/(app)/workspaces/[workspaceId]/actions";
import { MainNavigationHeader } from "@/app/(app)/workspaces/[workspaceId]/components/MainNavigationHeader";
import { MainNavigationNotices } from "@/app/(app)/workspaces/[workspaceId]/components/MainNavigationNotices";
import { NavigationLink } from "@/app/(app)/workspaces/[workspaceId]/components/NavigationLink";
import { SettingsSidebarContent } from "@/app/(app)/workspaces/[workspaceId]/components/SettingsSidebarContent";
import { useLatestStableRelease } from "@/app/(app)/workspaces/[workspaceId]/lib/use-latest-stable-release";
import { cn } from "@/lib/cn";
import { getBillingFallbackPath } from "@/lib/membership/navigation";
import { getAccessFlags } from "@/lib/membership/utils";
import { useSwitcherData } from "@/modules/settings/hooks/use-switcher-data";
import { Badge } from "@/modules/ui/components/badge";
import { GoBackButton } from "@/modules/ui/components/go-back-button";
import { ModalButton } from "@/modules/ui/components/upgrade-prompt";
import { CreateWorkspaceModal } from "@/modules/workspaces/components/create-workspace-modal";
import { WorkspaceLimitModal } from "@/modules/workspaces/components/workspace-limit-modal";

interface NavigationProps {
  organization: TOrganization;
  workspace: { id: string; name: string };
  isFormaCloud: boolean;
  isDevelopment: boolean;
  membershipRole?: TOrganizationRole;
  organizationWorkspacesLimit: number;
  isLicenseActive: boolean;
  isAccessControlAllowed: boolean;
  isContactsEnabled: boolean;
  areWorkflowsEnabled: boolean;
  responseCount: number;
  newTrialBannerVariant: string | boolean;
  // Whole days left in the trial, or null when there is no trial to count down. Computed by the
  // server layout: deriving it here would mean reading `Date.now()` during render, which diverges
  // between the server pass and hydration and then goes stale as the tab sits open (ENG-2366).
  trialDaysRemaining: number | null;
}

/**
 * A nav section header carrying a Beta badge.
 *
 * Analyze and Act are both pre-1.0 surfaces, and the badge is what tells someone the difference
 * between "this is finished" and "this is early". Extracted rather than duplicated so the two
 * sections cannot drift into looking subtly different from each other.
 */
const betaBadge = (
  <Badge
    text="Beta"
    type="gray"
    size="tiny"
    className="text-[10px] font-semibold tracking-normal normal-case"
  />
);

export const MainNavigation = ({
  organization,
  workspace,
  membershipRole,
  isFormaCloud,
  isDevelopment,
  organizationWorkspacesLimit,
  isLicenseActive,
  isAccessControlAllowed,
  isContactsEnabled,
  areWorkflowsEnabled,
  responseCount,
  newTrialBannerVariant,
  trialDaysRemaining,
}: Readonly<NavigationProps>) => {
  const router = useRouter();
  const pathname = usePathname();
  const { t } = useTranslation();
  const [isCollapsed, setIsCollapsed] = useState(false);
  const [isTextVisible, setIsTextVisible] = useState(true);

  const [, startTransition] = useTransition();
  const { isManager, isOwner, isBilling } = getAccessFlags(membershipRole);
  const isMembershipPending = membershipRole === undefined;
  const disabledNavigationMessage = isMembershipPending
    ? t("common.loading")
    : t("common.you_are_not_authorized_to_perform_this_action");

  const isOwnerOrManager = isManager || isOwner;
  const latestVersion = useLatestStableRelease(isOwnerOrManager);
  const isSettingsMode = pathname?.includes("/settings");

  const toggleSidebar = () => {
    setIsCollapsed(!isCollapsed);
    localStorage.setItem("isMainNavCollapsed", isCollapsed ? "false" : "true");
  };

  useEffect(() => {
    const isCollapsedValueFromLocalStorage = localStorage.getItem("isMainNavCollapsed") === "true";
    setIsCollapsed(isCollapsedValueFromLocalStorage);
  }, []);

  useEffect(() => {
    const toggleTextOpacity = () => {
      setIsTextVisible(isCollapsed);
    };
    const timeoutId = setTimeout(toggleTextOpacity, 150);
    return () => clearTimeout(timeoutId);
  }, [isCollapsed]);

  // One flat list rather than labelled sections. The labels were the remnant of an Ask / Analyze /
  // Act triad, and Analyze went with Dashboards and Unify Feedback in aef7841 — two headings over
  // three links was more chrome than content. The Beta mark moved onto Workflows, which is the
  // thing that is early; it had been sitting on a section that contained only Workflows anyway.
  const mainNavigationItems = useMemo(
    () => [
      {
        name: t("common.surveys"),
        href: `/workspaces/${workspace.id}/surveys`,
        icon: MessageCircle,
        isActive: pathname?.includes("/surveys"),
        isHidden: false,
        disabled: isMembershipPending || isBilling,
      },
      {
        href: `/workspaces/${workspace.id}/contacts`,
        name: t("common.contacts"),
        icon: UserIcon,
        isActive:
          pathname?.includes("/contacts") ||
          pathname?.includes("/segments") ||
          pathname?.includes("/attributes"),
        isHidden: !isContactsEnabled,
        disabled: isMembershipPending || isBilling,
      },
      {
        name: t("common.workflows"),
        href: `/workspaces/${workspace.id}/workflows`,
        icon: WorkflowIcon,
        isActive: pathname?.startsWith(`/workspaces/${workspace.id}/workflows`),
        isHidden: !areWorkflowsEnabled,
        disabled: isMembershipPending || isBilling,
        badge: betaBadge,
      },
    ],
    [t, workspace.id, pathname, isMembershipPending, isBilling, isContactsEnabled, areWorkflowsEnabled]
  );

  const visibleNavigationItems = useMemo(
    () => mainNavigationItems.filter((item) => !item.isHidden),
    [mainNavigationItems]
  );

  const settingsNavigationItem = useMemo(
    () => ({
      name: t("common.settings"),
      href: `/workspaces/${workspace.id}/settings/workspace/general`,
      icon: SettingsIcon,
      isActive: isSettingsMode,
      disabled: isMembershipPending || isBilling,
    }),
    [t, workspace.id, isSettingsMode, isMembershipPending, isBilling]
  );

  const workspaceSwitcher = useSwitcherData(
    () => getWorkspacesForSwitcherAction({ organizationId: organization.id }),
    t("common.failed_to_load_workspaces")
  );
  const organizationSwitcher = useSwitcherData(
    () => getOrganizationsForSwitcherAction({ organizationId: organization.id }),
    t("common.failed_to_load_organizations")
  );
  const [openCreateWorkspaceModal, setOpenCreateWorkspaceModal] = useState(false);
  const [openWorkspaceLimitModal, setOpenWorkspaceLimitModal] = useState(false);
  const mainNavigationLink = isBilling
    ? getBillingFallbackPath(organization.id, isFormaCloud)
    : `/workspaces/${workspace.id}/surveys/`;

  const workspaceLimitModalButtons = (): [ModalButton, ModalButton] => {
    if (isFormaCloud) {
      return [
        {
          text: t("workspace.settings.billing.upgrade"),
          href: `/organizations/${organization.id}/settings/billing`,
        },
        {
          text: t("common.cancel"),
          onClick: () => setOpenWorkspaceLimitModal(false),
        },
      ];
    }

    return [
      {
        text: t("workspace.settings.billing.upgrade"),
        href: isLicenseActive
          ? `/organizations/${organization.id}/settings/enterprise`
          : "https://forma.ylam.ai/upgrade-self-hosted-license?utm_source=forma-app&utm_medium=webapp&utm_campaign=upgrade_prompt_nav",
      },
      {
        text: t("common.cancel"),
        onClick: () => setOpenWorkspaceLimitModal(false),
      },
    ];
  };

  const handleSettingsWorkspaceChange = useCallback(
    (id: string) => {
      startTransition(() => {
        router.push(`/workspaces/${id}/settings/workspace/general`);
      });
    },
    [router]
  );

  const handleSettingsOrganizationChange = useCallback(
    (id: string) => {
      startTransition(() => {
        if (id === organization.id) {
          router.push(`/organizations/${organization.id}/settings/general`);
        } else {
          router.push(`/organizations/${id}/`);
        }
      });
    },
    [router, organization.id]
  );

  const mainNavIconClassName = "h-4 w-4 shrink-0";

  return (
    <>
      {workspace && (
        <aside
          className={cn(
            "z-40 flex flex-col justify-between rounded-r-xl border-r border-slate-200 bg-white pt-3 shadow-md transition-all duration-100",
            isSettingsMode || !isCollapsed ? "w-sidebar-collapsed" : "w-sidebar-expanded"
          )}>
          {isSettingsMode ? (
            <div className="flex flex-col overflow-hidden">
              <div className="mb-2 px-3">
                <GoBackButton url={`/workspaces/${workspace.id}/surveys`} />
              </div>

              {/* Settings sidebar content */}
              <SettingsSidebarContent
                workspaceId={workspace.id}
                workspaceName={workspace.name}
                organizationId={organization.id}
                organizationName={organization.name}
                membershipRole={membershipRole}
                isFormaCloud={isFormaCloud}
                isCollapsed={false}
                isTextVisible={false}
                workspaces={workspaceSwitcher.items}
                isLoadingWorkspaces={workspaceSwitcher.isLoading}
                onWorkspaceChange={handleSettingsWorkspaceChange}
                onWorkspaceDropdownOpen={() =>
                  workspaceSwitcher.error ? workspaceSwitcher.retry() : workspaceSwitcher.load()
                }
                errorWorkspaces={workspaceSwitcher.error}
                onWorkspaceRetry={workspaceSwitcher.retry}
                organizations={organizationSwitcher.items}
                isLoadingOrganizations={organizationSwitcher.isLoading}
                onOrganizationChange={handleSettingsOrganizationChange}
                onOrganizationDropdownOpen={() =>
                  organizationSwitcher.error ? organizationSwitcher.retry() : organizationSwitcher.load()
                }
                errorOrganizations={organizationSwitcher.error}
                onOrganizationRetry={organizationSwitcher.retry}
              />
            </div>
          ) : (
            <div>
              {/* Logo and Toggle */}

              <MainNavigationHeader
                isCollapsed={isCollapsed}
                isTextVisible={isTextVisible}
                homeHref={mainNavigationLink}
                onToggle={toggleSidebar}
              />

              {/* Main Nav */}
              <ul>
                {visibleNavigationItems.map((item) => (
                  <NavigationLink
                    key={item.name}
                    href={item.href}
                    isActive={item.isActive}
                    isCollapsed={isCollapsed}
                    isTextVisible={isTextVisible}
                    disabled={item.disabled}
                    disabledMessage={item.disabled ? disabledNavigationMessage : undefined}
                    badge={item.badge}
                    linkText={item.name}>
                    <item.icon className={mainNavIconClassName} strokeWidth={1.5} />
                  </NavigationLink>
                ))}
              </ul>
            </div>
          )}

          <div>
            {!isSettingsMode && (
              <MainNavigationNotices
                isCollapsed={isCollapsed}
                isOwnerOrManager={isOwnerOrManager}
                isFormaCloud={isFormaCloud}
                isDevelopment={isDevelopment}
                latestVersion={latestVersion}
                trialDaysRemaining={trialDaysRemaining}
                newTrialBannerVariant={newTrialBannerVariant}
                organization={organization}
                responseCount={responseCount}
              />
            )}

            {/* Settings is pinned to the foot of the sidebar rather than sitting at the end of the
                navigation list, so it stays in the same corner however long that list grows. The
                rest of this area emptied out when the account menu became the avatar at the top
                right and switching organization or workspace became the top bar's breadcrumb. */}
            {!isSettingsMode && (
              <div className="flex flex-col border-t border-slate-100 pt-2">
                <NavigationLink
                  href={settingsNavigationItem.href}
                  isActive={settingsNavigationItem.isActive}
                  isCollapsed={isCollapsed}
                  isTextVisible={isTextVisible}
                  disabled={settingsNavigationItem.disabled}
                  disabledMessage={settingsNavigationItem.disabled ? disabledNavigationMessage : undefined}
                  linkText={settingsNavigationItem.name}>
                  <settingsNavigationItem.icon className={mainNavIconClassName} strokeWidth={1.5} />
                </NavigationLink>
              </div>
            )}
          </div>
        </aside>
      )}
      {openWorkspaceLimitModal && (
        <WorkspaceLimitModal
          open={openWorkspaceLimitModal}
          setOpen={setOpenWorkspaceLimitModal}
          buttons={workspaceLimitModalButtons()}
          workspaceLimit={organizationWorkspacesLimit}
        />
      )}
      {openCreateWorkspaceModal && (
        <CreateWorkspaceModal
          open={openCreateWorkspaceModal}
          setOpen={setOpenCreateWorkspaceModal}
          organizationId={organization.id}
          isAccessControlAllowed={isAccessControlAllowed}
        />
      )}
    </>
  );
};
