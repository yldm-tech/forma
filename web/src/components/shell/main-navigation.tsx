import { Link } from "@tanstack/react-router";
import { MessageCircle, PanelLeftCloseIcon, PanelLeftOpenIcon, SettingsIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "../../lib/cn";
import { organizationSettingsPath, workspaceHomePath } from "../../lib/navigation";
import { NAV_COLLAPSED_KEY, readStorage, writeStorage } from "../../lib/storage";
import { Button } from "../ui/button";
import { Wordmark } from "../ui/logo";
import { NavigationLink } from "./navigation-link";

/**
 * forma2's MainNavigation for a workspace: the wordmark and collapse toggle, the workspace's areas, and Settings pinned to the foot. Entries are disabled, with the reason, while the membership is unknown or for the billing role. The areas of later modules join the list as they land.
 */
export const MainNavigation = ({
  workspaceId,
  organizationId,
  membershipRole,
  pathname,
}: {
  workspaceId: string;
  organizationId: string;
  membershipRole?: string;
  pathname: string;
}) => {
  const { t } = useTranslation();
  const [isCollapsed, setIsCollapsed] = useState(() => readStorage(NAV_COLLAPSED_KEY) === "true");
  const isPending = membershipRole === undefined;
  const isBilling = membershipRole === "billing";
  const disabledReason = isPending
    ? t("common.loading")
    : isBilling
      ? t("common.you_are_not_authorized_to_perform_this_action")
      : undefined;
  const homeHref = workspaceHomePath(workspaceId);
  const iconClassName = "h-4 w-4 shrink-0";

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
            <NavigationLink
              href={homeHref}
              isActive={pathname === homeHref || pathname.startsWith(`/workspaces/${workspaceId}/surveys`)}
              isCollapsed={isCollapsed}
              label={t("common.surveys")}
              icon={<MessageCircle className={iconClassName} strokeWidth={1.5} />}
              disabledReason={disabledReason}
            />
          </ul>
        </nav>
      </div>
      <ul className="flex flex-col border-t border-slate-100 pt-2">
        <NavigationLink
          href={organizationSettingsPath(organizationId, "general")}
          isActive={false}
          isCollapsed={isCollapsed}
          label={t("common.settings")}
          icon={<SettingsIcon className={iconClassName} strokeWidth={1.5} />}
          disabledReason={disabledReason}
        />
      </ul>
    </aside>
  );
};
