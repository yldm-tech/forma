import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { ArrowLeftIcon, Building2Icon, ChevronDownIcon, KeyIcon, UsersIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { accessFlags, type OrganizationRole } from "../../lib/roles";
import { organizationSettingsPath, organizationSwitchPath, sortByName } from "../../lib/navigation";
import { organizationsQuery } from "../../lib/queries/organizations";
import { DropdownMenu, DropdownMenuContent, DropdownMenuTrigger } from "../ui/dropdown-menu";
import { NavigationLink } from "./navigation-link";
import { SwitcherDropdownBody } from "./switcher-dropdown-body";

/**
 * forma2's settings sidebar for the organization and account settings pages: back to the workspace, and the organization's settings with a switcher beside the section label. General and teams are disabled for the billing role; API keys are offered to owners and managers only. Billing and domain settings are not part of this server.
 */
export const SettingsNavigation = ({
  organizationId,
  organizationName,
  membershipRole,
  backUrl,
  pathname,
}: {
  organizationId: string;
  organizationName: string;
  membershipRole?: OrganizationRole;
  backUrl: string;
  pathname: string;
}) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const organizations = useQuery({ ...organizationsQuery, enabled: open });
  const { isBilling, isOwnerOrManager } = accessFlags(membershipRole);
  const disabledReason = isBilling ? t("common.you_are_not_authorized_to_perform_this_action") : undefined;
  const iconClassName = "h-4 w-4 shrink-0";

  const items = [
    {
      id: "general",
      label: t("common.general"),
      href: organizationSettingsPath(organizationId, "general"),
      icon: <Building2Icon className={iconClassName} />,
      disabledReason,
    },
    {
      id: "teams",
      label: t("common.teams"),
      href: organizationSettingsPath(organizationId, "teams"),
      icon: <UsersIcon className={iconClassName} />,
      disabledReason,
    },
    ...(isOwnerOrManager
      ? [
          {
            id: "api-keys",
            label: t("common.api_keys"),
            href: organizationSettingsPath(organizationId, "api-keys"),
            icon: <KeyIcon className={iconClassName} />,
            disabledReason: undefined,
          },
        ]
      : []),
  ];

  return (
    <aside className="z-40 flex w-sidebar-collapsed shrink-0 flex-col justify-between rounded-r-xl border-r border-slate-200 bg-white pt-3 shadow-md">
      <div className="flex flex-col overflow-hidden">
        <div className="mb-2 px-3">
          <Link
            to={backUrl}
            className="inline-flex items-center gap-2 rounded-md px-2 py-1 text-sm text-slate-600 hover:bg-slate-50 hover:text-slate-900">
            <ArrowLeftIcon className="size-4" />
            {t("common.back")}
          </Link>
        </div>
        <div className="mt-4 mb-1 flex min-w-0 items-center gap-2 px-4">
          <span className="shrink-0 text-xs font-semibold tracking-wider text-slate-500 uppercase">
            {t("common.organization")}
          </span>
          <DropdownMenu onOpenChange={setOpen}>
            <DropdownMenuTrigger className="ml-auto flex max-w-[50%] min-w-0 items-center gap-1 rounded-md border border-slate-200 px-2 py-0.5 text-xs text-slate-600 hover:bg-slate-50">
              <span className="truncate">{organizationName}</span>
              <ChevronDownIcon className="size-3" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="max-h-[300px]">
              <SwitcherDropdownBody
                type="organization"
                isLoading={organizations.isLoading}
                isError={organizations.isError}
                onRetry={() => void organizations.refetch()}
                items={sortByName(organizations.data ?? [])}
                selectedId={organizationId}
                onSelect={(id) => void navigate({ href: organizationSwitchPath(id, organizationId) })}
              />
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
        <nav aria-label={t("common.settings")}>
          <ul className="space-y-0.5">
            {items.map((item) => (
              <NavigationLink
                key={item.id}
                href={item.href}
                isActive={pathname.includes(item.href)}
                label={item.label}
                icon={item.icon}
                disabledReason={item.disabledReason}
                indent
              />
            ))}
          </ul>
        </nav>
      </div>
    </aside>
  );
};
