import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import {
  BlocksIcon,
  MessageCircle,
  MousePointerClickIcon,
  PanelLeftCloseIcon,
  PanelLeftOpenIcon,
  RocketIcon,
  SettingsIcon,
  TagIcon,
  UserIcon,
  WorkflowIcon,
} from "lucide-react";
import type { TFunction } from "i18next";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { TrialAlert } from "../../features/billing/components/trial-alert";
import { cn } from "../../lib/cn";
import { organizationSettingsPath, workspaceHomePath, workspaceSettingsPath } from "../../lib/navigation";
import { billingSummaryQuery } from "../../lib/queries/billing";
import {
  DEVELOPMENT_VERSION,
  instanceQuery,
  isNewerVersion,
  latestReleaseQuery,
} from "../../lib/queries/instance";
import { NAV_COLLAPSED_KEY, readStorage, writeStorage } from "../../lib/storage";
import { Button } from "../ui/button";
import { Wordmark } from "../ui/logo";
import { NavigationLink } from "./navigation-link";

/** forma2's self-hosted update notice (MainNavigationNotices): a released, self-hosted instance whose latest stable Forma release is ahead of the running one links to the releases. Neither Forma Cloud nor a development build shows it. */
const UpdateNotice = ({ organizationId }: { organizationId: string }) => {
  const { t } = useTranslation();
  const { data: instance } = useQuery(instanceQuery);
  const { data: billing } = useQuery(billingSummaryQuery(organizationId));
  const isReleasedSelfHosted =
    billing?.isFormaCloud === false && instance !== undefined && instance.version !== DEVELOPMENT_VERSION;
  const { data: latestVersion } = useQuery({ ...latestReleaseQuery, enabled: isReleasedSelfHosted });
  if (!isReleasedSelfHosted || !latestVersion || !isNewerVersion(`v${instance.version}`, latestVersion)) {
    return null;
  }
  return (
    <a
      href="https://github.com/yldm-tech/forma/releases"
      target="_blank"
      rel="noopener noreferrer"
      className="m-2 flex items-center gap-x-4 rounded-lg border border-slate-200 bg-slate-100 p-2 text-sm text-slate-800 hover:border-slate-300 hover:bg-slate-200">
      <p className="flex items-center justify-center gap-x-2 text-xs">
        <RocketIcon strokeWidth={1.5} className="mx-1 size-6 text-slate-900" />
        {t("common.new_version_available", { version: latestVersion })}
      </p>
    </a>
  );
};

/** Why the workspace's entries are disabled: the membership is still loading, or it is the billing role's, which sees no survey work. Undefined when they are not. */
const navigationDisabledReason = (t: TFunction, membershipRole: string | undefined): string | undefined => {
  if (membershipRole === undefined) return t("common.loading");
  if (membershipRole === "billing") return t("common.you_are_not_authorized_to_perform_this_action");
  return undefined;
};

/** The workspace's areas in sidebar order, each marked active for the pages under it. isPhoneReady marks the areas whose pages open on a phone, the only ones the phone's navigation drawer lists. */
const workspaceAreas = (t: TFunction, workspaceId: string, pathname: string) => {
  const homeHref = workspaceHomePath(workspaceId);
  const base = `/workspaces/${workspaceId}`;
  return [
    {
      href: homeHref,
      label: t("common.surveys"),
      icon: MessageCircle,
      isActive: pathname === homeHref || pathname.startsWith(`${base}/surveys`),
      isPhoneReady: true,
    },
    {
      href: `${base}/contacts`,
      label: t("common.contacts"),
      icon: UserIcon,
      isActive:
        pathname.startsWith(`${base}/contacts`) ||
        pathname.startsWith(`${base}/segments`) ||
        pathname.startsWith(`${base}/attributes`),
      isPhoneReady: true,
    },
    {
      href: `${base}/workflows`,
      label: t("common.workflows"),
      icon: WorkflowIcon,
      isActive: pathname.startsWith(`${base}/workflows`),
      isPhoneReady: false,
    },
    {
      href: `${base}/user-actions`,
      label: t("common.user_actions"),
      icon: MousePointerClickIcon,
      isActive: pathname.startsWith(`${base}/user-actions`),
      isPhoneReady: false,
    },
    {
      href: `${base}/integrations`,
      label: t("common.integrations"),
      icon: BlocksIcon,
      isActive: pathname.startsWith(`${base}/integrations`),
      isPhoneReady: false,
    },
    {
      href: `${base}/tags`,
      label: t("common.tags"),
      icon: TagIcon,
      isActive: pathname.startsWith(`${base}/tags`),
      isPhoneReady: false,
    },
  ];
};

/**
 * forma2's MainNavigation for a workspace: the wordmark and collapse toggle, the workspace's areas as one flat list, and Settings pinned to the foot. Entries are disabled, with the reason, while the membership is unknown or for the billing role. The areas of later modules join the list as they land. Above Settings an owner or manager sees forma2's MainNavigationNotices: self-hosted, the new-release notice; on Forma Cloud, the trial's countdown linking to billing.
 */
export const MainNavigation = ({
  workspaceId,
  organizationId,
  membershipRole,
  pathname,
  trialDaysRemaining = null,
}: {
  workspaceId: string;
  organizationId: string;
  membershipRole?: string;
  pathname: string;
  /** Whole days left in the organization's Forma Cloud trial, null without one. */
  trialDaysRemaining?: number | null;
}) => {
  const { t } = useTranslation();
  const [isCollapsed, setIsCollapsed] = useState(() => readStorage(NAV_COLLAPSED_KEY) === "true");
  const isOwnerOrManager = membershipRole === "owner" || membershipRole === "manager";
  const disabledReason = navigationDisabledReason(t, membershipRole);
  const homeHref = workspaceHomePath(workspaceId);
  const iconClassName = "h-4 w-4 shrink-0";
  const items = workspaceAreas(t, workspaceId, pathname);

  const toggle = () => {
    writeStorage(NAV_COLLAPSED_KEY, isCollapsed ? "false" : "true");
    setIsCollapsed(!isCollapsed);
  };

  return (
    <aside
      className={cn(
        "z-40 flex flex-col justify-between rounded-r-xl border-r border-slate-200 bg-white pt-3 shadow-md transition-all duration-100",
        isCollapsed ? "w-sidebar-expanded" : "w-sidebar-collapsed"
      )}>
      <div>
        <div
          className={cn("flex items-center px-3 pb-4", isCollapsed ? "justify-center" : "justify-between")}>
          {!isCollapsed && (
            <Link to={homeHref} className="flex items-center justify-center">
              <Wordmark className="text-2xl" />
            </Link>
          )}
          <Button
            variant="ghost"
            size="icon"
            onClick={toggle}
            aria-label={isCollapsed ? t("common.expand_navigation") : t("common.collapse_navigation")}
            aria-expanded={!isCollapsed}
            className="rounded-xl bg-slate-50 p-1 text-slate-600 hover:bg-slate-100">
            {isCollapsed ? <PanelLeftOpenIcon strokeWidth={1.5} /> : <PanelLeftCloseIcon strokeWidth={1.5} />}
          </Button>
        </div>
        <nav aria-label={t("common.workspace")}>
          <ul>
            {items.map((item) => (
              <NavigationLink
                key={item.href}
                href={item.href}
                isActive={item.isActive}
                isCollapsed={isCollapsed}
                label={item.label}
                icon={<item.icon className={iconClassName} strokeWidth={1.5} />}
                disabledReason={disabledReason}
              />
            ))}
          </ul>
        </nav>
      </div>
      <div>
        {!isCollapsed && isOwnerOrManager && <UpdateNotice organizationId={organizationId} />}
        {!isCollapsed && isOwnerOrManager && trialDaysRemaining !== null && (
          <Link to={organizationSettingsPath(organizationId, "billing")} className="m-2 block">
            <TrialAlert trialDaysRemaining={trialDaysRemaining} size="small" />
          </Link>
        )}
        <ul className="flex flex-col border-t border-slate-100 pt-2">
          <NavigationLink
            href={workspaceSettingsPath(workspaceId, "general")}
            isActive={pathname.includes("/settings")}
            isCollapsed={isCollapsed}
            label={t("common.settings")}
            icon={<SettingsIcon className={iconClassName} strokeWidth={1.5} />}
            disabledReason={disabledReason}
          />
        </ul>
      </div>
    </aside>
  );
};

/**
 * The workspace's navigation in the phone's drawer: the areas whose pages open on a phone, the survey list and the contacts, with a note that the others need a bigger screen. Entries are disabled, with the reason, as in the sidebar. Following one calls onNavigate, so the drawer closes even when the entry is the page already open.
 */
export const MobileNavigation = ({
  workspaceId,
  membershipRole,
  pathname,
  onNavigate,
}: {
  workspaceId: string;
  membershipRole?: string;
  pathname: string;
  onNavigate: () => void;
}) => {
  const { t } = useTranslation();
  const disabledReason = navigationDisabledReason(t, membershipRole);
  const items = workspaceAreas(t, workspaceId, pathname).filter((item) => item.isPhoneReady);
  return (
    <div className="-mx-5 flex flex-col gap-4">
      <nav aria-label={t("common.workspace")}>
        <ul>
          {items.map((item) => (
            <NavigationLink
              key={item.href}
              href={item.href}
              isActive={item.isActive}
              label={item.label}
              icon={<item.icon className="h-4 w-4 shrink-0" strokeWidth={1.5} />}
              disabledReason={disabledReason}
              onClick={onNavigate}
            />
          ))}
        </ul>
      </nav>
      <p className="px-5 text-sm text-slate-500">{t("common.mobile_navigation_more_on_desktop")}</p>
    </div>
  );
};
