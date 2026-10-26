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
import { organizationsQuery } from "../../lib/queries/organizations";
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

/** forma2's OrganizationBreadcrumb: the current organization, a switcher loaded when it opens with "Create new organization" under the list (any signed-in user may create one, as the workspace switcher offers "Add new workspace"), and a link to its settings. */
export const OrganizationBreadcrumb = ({
  organizationId,
  organizationName,
  isLastCrumb,
}: {
  organizationId: string;
  organizationName: string;
  isLastCrumb: boolean;
}) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);
  const organizations = useQuery({ ...organizationsQuery, enabled: open });

  return (
    <BreadcrumbItem isActive={open}>
      <DropdownMenu onOpenChange={setOpen}>
        <DropdownMenuTrigger className="focus-visible:ring-ring flex cursor-pointer items-center gap-1 rounded-xs focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-hidden">
          <Building2Icon className="size-3" strokeWidth={1.5} />
          <span>{organizationName}</span>
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
            onSelect={(id) => void navigate({ href: organizationSwitchPath(id, organizationId) })}>
            <DropdownMenuCheckboxItem className="w-full justify-between" onSelect={() => setCreateOpen(true)}>
              <span>{t("workspace.settings.general.create_new_organization")}</span>
              <PlusIcon className="ml-2 size-4" strokeWidth={1.5} />
            </DropdownMenuCheckboxItem>
          </SwitcherDropdownBody>
          <DropdownMenuSeparator />
          <DropdownMenuCheckboxItem
            onSelect={() => void navigate({ href: organizationSettingsPath(organizationId, "general") })}>
            <SettingsIcon className="mr-2 size-4" />
            {t("common.settings")}
          </DropdownMenuCheckboxItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <CreateOrganizationModal open={createOpen} setOpen={setCreateOpen} />
    </BreadcrumbItem>
  );
};
