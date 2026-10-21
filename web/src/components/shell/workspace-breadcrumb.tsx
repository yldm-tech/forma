import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { ChevronDownIcon, FoldersIcon, PlusIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { sortByName, workspaceHomePath } from "../../lib/navigation";
import { workspacesQuery } from "../../lib/queries/workspaces";
import { BreadcrumbItem } from "../ui/breadcrumb";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "../ui/dropdown-menu";
import { DisabledReason } from "../ui/popover";
import { CreateWorkspaceModal } from "./create-workspace-modal";
import { SwitcherDropdownBody } from "./switcher-dropdown-body";

/** forma2's WorkspaceBreadcrumb: the current workspace, a switcher loaded when it opens, and "add new workspace" for owners and managers. */
export const WorkspaceBreadcrumb = ({
  workspaceId,
  workspaceName,
  organizationId,
  isOwnerOrManager,
  isMembershipPending,
}: {
  workspaceId: string;
  workspaceName: string;
  organizationId: string;
  isOwnerOrManager: boolean;
  isMembershipPending: boolean;
}) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);
  const workspaces = useQuery({ ...workspacesQuery(organizationId), enabled: open });

  return (
    <BreadcrumbItem isActive={open}>
      <DropdownMenu onOpenChange={setOpen}>
        <DropdownMenuTrigger className="flex cursor-pointer items-center gap-1 outline-hidden">
          <FoldersIcon className="size-3" strokeWidth={1.5} />
          <span>{workspaceName}</span>
          <ChevronDownIcon className="size-3" strokeWidth={1.5} />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="mt-2">
          <SwitcherDropdownBody
            type="workspace"
            isLoading={workspaces.isLoading}
            isError={workspaces.isError}
            onRetry={() => void workspaces.refetch()}
            items={sortByName(workspaces.data ?? [])}
            selectedId={workspaceId}
            onSelect={(id) => void navigate({ href: workspaceHomePath(id) })}>
            {isMembershipPending || !isOwnerOrManager ? (
              <DisabledReason
                reason={
                  isMembershipPending
                    ? t("common.loading")
                    : t("common.you_are_not_authorized_to_perform_this_action")
                }>
                <button
                  type="button"
                  aria-disabled="true"
                  className="relative flex w-full cursor-not-allowed items-center justify-between rounded-lg py-1.5 pr-2 pl-8 text-sm font-medium text-slate-400 select-none">
                  <span>{t("common.add_new_workspace")}</span>
                  <PlusIcon className="ml-2 size-4" strokeWidth={1.5} />
                </button>
              </DisabledReason>
            ) : (
              <DropdownMenuCheckboxItem
                className="w-full justify-between"
                onSelect={() => setCreateOpen(true)}>
                <span>{t("common.add_new_workspace")}</span>
                <PlusIcon className="ml-2 size-4" strokeWidth={1.5} />
              </DropdownMenuCheckboxItem>
            )}
          </SwitcherDropdownBody>
        </DropdownMenuContent>
      </DropdownMenu>
      <CreateWorkspaceModal open={createOpen} setOpen={setCreateOpen} organizationId={organizationId} />
    </BreadcrumbItem>
  );
};
