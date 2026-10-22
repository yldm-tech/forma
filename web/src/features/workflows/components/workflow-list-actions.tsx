import { useNavigate } from "@tanstack/react-router";
import {
  ArchiveIcon,
  ArchiveRestoreIcon,
  CopyIcon,
  MoreVertical,
  SquarePenIcon,
  TrashIcon,
} from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ConfirmationModal } from "../../../components/ui/confirmation-modal";
import { DeleteDialog } from "../../../components/ui/delete-dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "../../../components/ui/dropdown-menu";
import { toast } from "../../../lib/toast";
import { type TWorkflowListKey, getWorkflowApiErrorMessage } from "../api";
import type { TWorkflowStatus } from "../definition";
import {
  useArchiveWorkflow,
  useDeleteWorkflow,
  useDuplicateWorkflow,
  useUnarchiveWorkflow,
} from "../hooks/queries";

interface WorkflowListActionsProps {
  workflowId: string;
  workflowName: string;
  status: TWorkflowStatus;
  workspaceId: string;
  isReadOnly: boolean;
  queryKey: TWorkflowListKey;
}

/** forma2's WorkflowListActions: a row's menu with edit, duplicate, archive or unarchive, and delete, which only an archived workflow offers. */
export const WorkflowListActions = ({
  workflowId,
  workflowName,
  status,
  workspaceId,
  isReadOnly,
  queryKey,
}: Readonly<WorkflowListActionsProps>) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [isMenuOpen, setIsMenuOpen] = useState(false);
  const [isArchiveDialogOpen, setIsArchiveDialogOpen] = useState(false);
  const [isDeleteDialogOpen, setIsDeleteDialogOpen] = useState(false);

  const archiveWorkflowMutation = useArchiveWorkflow({ queryKey });
  const unarchiveWorkflowMutation = useUnarchiveWorkflow();
  const deleteWorkflowMutation = useDeleteWorkflow({ queryKey });
  const duplicateWorkflowMutation = useDuplicateWorkflow();

  // Every action here changes the workflow, so a read-only member gets no menu; the row still opens the workflow.
  if (isReadOnly) return null;

  const isArchived = status === "archived";

  const handleDuplicate = () =>
    duplicateWorkflowMutation.mutate(
      { workflowId },
      {
        onSuccess: () => toast.success(t("workspace.workflows.duplicate_success")),
        onError: (error) =>
          toast.error(getWorkflowApiErrorMessage(error, t("workspace.workflows.duplicate_failed"))),
      }
    );

  const handleArchive = () =>
    archiveWorkflowMutation.mutate(
      { workflowId },
      {
        onSuccess: () => {
          toast.success(t("workspace.workflows.archive_success"));
          setIsArchiveDialogOpen(false);
        },
        onError: (error) =>
          toast.error(getWorkflowApiErrorMessage(error, t("workspace.workflows.archive_failed"))),
      }
    );

  const handleUnarchive = () =>
    unarchiveWorkflowMutation.mutate(
      { workflowId },
      {
        onSuccess: () => toast.success(t("workspace.workflows.unarchive_success")),
        onError: (error) =>
          toast.error(getWorkflowApiErrorMessage(error, t("workspace.workflows.unarchive_failed"))),
      }
    );

  const handleDelete = () =>
    deleteWorkflowMutation.mutate(
      { workflowId },
      {
        onSuccess: () => {
          toast.success(t("workspace.workflows.delete_success"));
          setIsDeleteDialogOpen(false);
        },
        onError: (error) =>
          toast.error(getWorkflowApiErrorMessage(error, t("workspace.workflows.delete_failed"))),
      }
    );

  return (
    <div data-testid="workflow-dropdown-menu">
      <DropdownMenu open={isMenuOpen} onOpenChange={setIsMenuOpen}>
        <DropdownMenuTrigger className="z-10" asChild>
          <button
            type="button"
            aria-label={t("common.open_options")}
            className="cursor-pointer rounded-lg border bg-white p-2 hover:bg-slate-50">
            <MoreVertical className="size-4" aria-hidden="true" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent className="inline-block w-auto min-w-max" align="end">
          <DropdownMenuGroup>
            <DropdownMenuItem
              icon={<SquarePenIcon className="size-4" />}
              onSelect={() => void navigate({ href: `/workspaces/${workspaceId}/workflows/${workflowId}` })}>
              {t("common.edit")}
            </DropdownMenuItem>
            <DropdownMenuItem
              icon={<CopyIcon className="size-4" />}
              disabled={duplicateWorkflowMutation.isPending}
              onSelect={(event) => {
                event.preventDefault();
                setIsMenuOpen(false);
                handleDuplicate();
              }}>
              {t("common.duplicate")}
            </DropdownMenuItem>
            {isArchived ? (
              <DropdownMenuItem
                icon={<ArchiveRestoreIcon className="size-4" />}
                disabled={unarchiveWorkflowMutation.isPending}
                onSelect={(event) => {
                  event.preventDefault();
                  setIsMenuOpen(false);
                  handleUnarchive();
                }}>
                {t("workspace.workflows.unarchive")}
              </DropdownMenuItem>
            ) : (
              <DropdownMenuItem
                icon={<ArchiveIcon className="size-4" />}
                onSelect={(event) => {
                  event.preventDefault();
                  setIsMenuOpen(false);
                  setIsArchiveDialogOpen(true);
                }}>
                {t("common.archive")}
              </DropdownMenuItem>
            )}
            {/* Delete waits behind archiving, a guard against accidental hard deletes. */}
            {isArchived ? (
              <DropdownMenuItem
                icon={<TrashIcon className="size-4" />}
                onSelect={(event) => {
                  event.preventDefault();
                  setIsMenuOpen(false);
                  setIsDeleteDialogOpen(true);
                }}>
                {t("common.delete")}
              </DropdownMenuItem>
            ) : null}
          </DropdownMenuGroup>
        </DropdownMenuContent>
      </DropdownMenu>

      <ConfirmationModal
        open={isArchiveDialogOpen}
        setOpen={setIsArchiveDialogOpen}
        title={t("workspace.workflows.archive_workflow")}
        description={t("workspace.workflows.archive_workflow_description")}
        body={t("workspace.workflows.archive_workflow_confirmation", { name: workflowName })}
        buttonText={t("common.archive")}
        onConfirm={handleArchive}
        buttonLoading={archiveWorkflowMutation.isPending}
        Icon={ArchiveIcon}
      />
      <DeleteDialog
        open={isDeleteDialogOpen}
        setOpen={setIsDeleteDialogOpen}
        deleteWhat={workflowName}
        onDelete={handleDelete}
        isDeleting={deleteWorkflowMutation.isPending}
        text={t("workspace.workflows.delete_workflow_confirmation", { name: workflowName })}
      />
    </div>
  );
};
