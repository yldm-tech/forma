import { Link } from "@tanstack/react-router";
import {
  BlocksIcon,
  MessageCircle,
  MousePointerClickIcon,
  PanelLeftCloseIcon,
  PanelLeftOpenIcon,
  SettingsIcon,
  TagIcon,
  UserIcon,
  WorkflowIcon,
} from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { TrialAlert } from "../../features/billing/components/trial-alert";
import { cn } from "../../lib/cn";
import { organizationSettingsPath, workspaceHomePath, workspaceSettingsPath } from "../../lib/navigation";
import { NAV_COLLAPSED_KEY, readStorage, writeStorage } from "../../lib/storage";
import { Button } from "../ui/button";
import { Wordmark } from "../ui/logo";
import { NavigationLink } from "./navigation-link";

/**
 * forma2's MainNavigation for a workspace: the wordmark and collapse toggle, the workspace's areas as one flat list, and Settings pinned to the foot. Entries are disabled, with the reason, while the membership is unknown or for the billing role. The areas of later modules join the list as they land. On Forma Cloud an owner or manager sees the trial's countdown above Settings, linking to billing (forma2's MainNavigationNotices).
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
  const isPending = membershipRole === undefined;
  const isBilling = membershipRole === "billing";
  const isOwnerOrManager = membershipRole === "owner" || membershipRole === "manager";
  const disabledReason = isPending
    ? t("common.loading")
    : isBilling
      ? t("common.you_are_not_authorized_to_perform_this_action")
      : undefined;
  const homeHref = workspaceHomePath(workspaceId);
  const base = `/workspaces/${workspaceId}`;
  const iconClassName = "h-4 w-4 shrink-0";
  const items = [
    {
      href: homeHref,
      label: t("common.surveys"),
      icon: MessageCircle,
      isActive: pathname === homeHref || pathname.startsWith(`${base}/surveys`),
    },
    {
      href: `${base}/contacts`,
      label: t("common.contacts"),
      icon: UserIcon,
      isActive:
        pathname.startsWith(`${base}/contacts`) ||
        pathname.startsWith(`${base}/segments`) ||
        pathname.startsWith(`${base}/attributes`),
    },
    {
      href: `${base}/workflows`,
      label: t("common.workflows"),
      icon: WorkflowIcon,
      isActive: pathname.startsWith(`${base}/workflows`),
    },
    {
      href: `${base}/user-actions`,
      label: t("common.user_actions"),
      icon: MousePointerClickIcon,
      isActive: pathname.startsWith(`${base}/user-actions`),
    },
    {
      href: `${base}/integrations`,
      label: t("common.integrations"),
      icon: BlocksIcon,
      isActive: pathname.startsWith(`${base}/integrations`),
    },
    {
      href: `${base}/tags`,
      label: t("common.tags"),
      icon: TagIcon,
      isActive: pathname.startsWith(`${base}/tags`),
    },
  ];

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
