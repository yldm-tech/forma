import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Building2Icon, ChevronDownIcon, ChevronRightIcon, PlusIcon, SettingsIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  organizationSettingsPath,
  organizationSwitchPath,
  switcherOrganizations,
} from "../../lib/navigation";
import { organizationCreationQuery, organizationsQuery } from "../../lib/queries/organizations";
import { BreadcrumbItem } from "../ui/breadcrumb";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "../ui/dropdown-menu";
import { CreateOrganizationModal } from "./create-organization-modal";
import { SwitcherDropdownBody } from "./switcher-dropdown-body";

/**
 * forma2's OrganizationBreadcrumb: the current organization, a switcher loaded when it opens with "Create new organization" under the list (as the workspace switcher offers "Add new workspace") to whoever may create one, which on an invite-only instance is only the owner of an organization, and a link to its settings.
 *
 * On a phone, where the admin is read-only, it is only the switcher: another organization opens on its survey list, which a phone opens, while creating one and the settings do not appear, and choosing the current organization (which otherwise opens its settings) stays put. Before the workspace crumb it shows only its icon there, to leave the narrow bar room; the name stays its accessible name.
 */
export const OrganizationBreadcrumb = ({
  organizationId,
  organizationName,
  isLastCrumb,
  isPhone = false,
}: {
  organizationId: string;
  organizationName: string;
  isLastCrumb: boolean;
  isPhone?: boolean;
}) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);
  const organizations = useQuery({ ...organizationsQuery, enabled: open });
  const canCreate = useQuery({ ...organizationCreationQuery, enabled: open && !isPhone }).data === true;

  return (
    <BreadcrumbItem isActive={open}>
      <DropdownMenu onOpenChange={setOpen}>
        <DropdownMenuTrigger className="focus-visible:ring-ring flex cursor-pointer items-center gap-1 rounded-xs focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-hidden">
          <Building2Icon className="size-3" strokeWidth={1.5} />
          <span className={isPhone && !isLastCrumb ? "sr-only" : undefined}>{organizationName}</span>
          {open || isLastCrumb ? (
            <ChevronDownIcon className="size-3" strokeWidth={1.5} />
          ) : (
            <ChevronRightIcon className="size-3" strokeWidth={1.5} />
          )}
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="mt-2">
          <SwitcherDropdownBody
            type="organization"
            isLoading={organizations.isLoading}
            isError={organizations.isError}
            onRetry={() => void organizations.refetch()}
            items={switcherOrganizations(t, organizations.data ?? [])}
            selectedId={organizationId}
            onSelect={(id) => {
              if (isPhone && id === organizationId) return;
              void navigate({ href: organizationSwitchPath(id, organizationId) });
            }}>
            {!isPhone && canCreate && (
              <DropdownMenuCheckboxItem
                className="w-full justify-between"
                onSelect={() => setCreateOpen(true)}>
                <span>{t("workspace.settings.general.create_new_organization")}</span>
                <PlusIcon className="ml-2 size-4" strokeWidth={1.5} />
              </DropdownMenuCheckboxItem>
            )}
          </SwitcherDropdownBody>
          {!isPhone && (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuCheckboxItem
                onSelect={() => void navigate({ href: organizationSettingsPath(organizationId, "general") })}>
                <SettingsIcon className="mr-2 size-4" />
                {t("common.settings")}
              </DropdownMenuCheckboxItem>
            </>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
      {!isPhone && <CreateOrganizationModal open={createOpen} setOpen={setCreateOpen} />}
    </BreadcrumbItem>
  );
};
