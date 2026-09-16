"use client";

import { MessageCircle, SettingsIcon, UserIcon, WorkflowIcon } from "lucide-react";
import { usePathname, useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useState, useTransition } from "react";
import { useTranslation } from "react-i18next";
import { TOrganizationRole } from "@forma/types/memberships";
import { TOrganization } from "@forma/types/organizations";
import { TUser } from "@forma/types/user";
import {
  getOrganizationsForSwitcherAction,
  getWorkspacesForSwitcherAction,
} from "@/app/(app)/workspaces/[workspaceId]/actions";
import { MainNavigationHeader } from "@/app/(app)/workspaces/[workspaceId]/components/MainNavigationHeader";
import { MainNavigationNotices } from "@/app/(app)/workspaces/[workspaceId]/components/MainNavigationNotices";
import { NavigationLink } from "@/app/(app)/workspaces/[workspaceId]/components/NavigationLink";
import { SettingsSidebarContent } from "@/app/(app)/workspaces/[workspaceId]/components/SettingsSidebarContent";
import { getVisibleNavigationSections } from "@/app/(app)/workspaces/[workspaceId]/lib/navigation-visibility";
import { useLatestStableRelease } from "@/app/(app)/workspaces/[workspaceId]/lib/use-latest-stable-release";
import { cn } from "@/lib/cn";
import { getBillingFallbackPath } from "@/lib/membership/navigation";
import { getAccessFlags } from "@/lib/membership/utils";
import { UserDropdown } from "@/modules/settings/components/user-dropdown";
import { useSwitcherData } from "@/modules/settings/hooks/use-switcher-data";
import { Badge } from "@/modules/ui/components/badge";
import { GoBackButton } from "@/modules/ui/components/go-back-button";
import { ModalButton } from "@/modules/ui/components/upgrade-prompt";
import { CreateWorkspaceModal } from "@/modules/workspaces/components/create-workspace-modal";
import { WorkspaceLimitModal } from "@/modules/workspaces/components/workspace-limit-modal";

interface NavigationProps {
  user: TUser;
  organization: TOrganization;
  workspace: { id: string; name: string };
  isFormaCloud: boolean;
  isDevelopment: boolean;
  membershipRole?: TOrganizationRole;
  publicDomain: string;
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
const sectionLabelWithBeta = (label: React.ReactNode) => (
  <span className="inline-flex items-center gap-2">
    <span>{label}</span>
    <Badge
      text="Beta"
      type="gray"
      size="tiny"
      className="text-[10px] font-semibold tracking-normal normal-case"
    />
  </span>
);

export const MainNavigation = ({
  organization,
  user,
  workspace,
  membershipRole,
  isFormaCloud,
  isDevelopment,
  publicDomain,
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

  const mainNavigationSections = useMemo(
    () => [
      {
        id: "ask",
        // Product section (IA) label — intentionally not localized (kept in English across all locales)
        name: "Ask",
        items: [
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
        ],
      },
      {
        id: "act",
        // Kept translated, unlike "Ask" and "Analyze" above. Those two are deliberately English in
        // every locale; this one has been going through t() since it was added. Making the three
        // consistent means dropping a string 15 locales already translate, which is a naming
        // decision rather than a side effect of adding a badge — see ENG-2742.
        name: sectionLabelWithBeta(t("common.act")),
        items: [
          {
            name: t("common.workflows"),
            href: `/workspaces/${workspace.id}/workflows`,
            icon: WorkflowIcon,
            isActive: pathname?.startsWith(`/workspaces/${workspace.id}/workflows`),
            isHidden: !areWorkflowsEnabled,
            disabled: isMembershipPending || isBilling,
          },
        ],
      },
    ],
    [t, workspace.id, pathname, isMembershipPending, isBilling, isContactsEnabled, areWorkflowsEnabled]
  );

  const visibleNavigationSections = useMemo(
    () => getVisibleNavigationSections(mainNavigationSections),
    [mainNavigationSections]
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
              <ul className="space-y-2">
                {visibleNavigationSections.map((section) => (
                  <li key={section.id}>
                    {!isCollapsed && !isTextVisible && (
                      <p className="px-4 pt-2 pb-1 text-xs font-semibold tracking-wide text-slate-400 uppercase">
                        {section.name}
                      </p>
                    )}

                    <ul>
                      {section.items.map(
                        (item) =>
                          !item.isHidden && (
                            <NavigationLink
                              key={item.name}
                              href={item.href}
                              isActive={item.isActive}
                              isCollapsed={isCollapsed}
                              isTextVisible={isTextVisible}
                              disabled={item.disabled}
                              disabledMessage={item.disabled ? disabledNavigationMessage : undefined}
                              linkText={item.name}>
                              <item.icon className={mainNavIconClassName} strokeWidth={1.5} />
                            </NavigationLink>
                          )
                      )}
                    </ul>
                  </li>
                ))}

                <li className={cn("mt-2 border-t border-slate-100 pt-2", isCollapsed && "border-t-0 pt-0")}>
                  <ul>
                    <NavigationLink
                      href={settingsNavigationItem.href}
                      isActive={settingsNavigationItem.isActive}
                      isCollapsed={isCollapsed}
                      isTextVisible={isTextVisible}
                      disabled={settingsNavigationItem.disabled}
                      disabledMessage={
                        settingsNavigationItem.disabled ? disabledNavigationMessage : undefined
                      }
                      linkText={settingsNavigationItem.name}>
                      <settingsNavigationItem.icon className={mainNavIconClassName} strokeWidth={1.5} />
                    </NavigationLink>
                  </ul>
                </li>
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

            <div className="flex flex-col">
              {/* Organization and workspace switching lives in the top bar's breadcrumb
                  (`WorkspaceAndOrgSwitch`), which renders on every page through `WorkspaceLayout`
                  and `settings-shell`, and states the relationship — org › workspace — that two
                  stacked dropdowns here could only imply. Keeping both meant two places to change
                  and two fetches of the same switcher data. Creating a workspace lives there too. */}

              <UserDropdown
                user={user}
                organizationId={organization.id}
                publicDomain={publicDomain}
                isCollapsed={isCollapsed}
                isTextVisible={isTextVisible}
                className="rounded-br-xl"
              />
            </div>
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
