import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useAtomValue } from "jotai";
import {
  ArchiveIcon,
  ArchiveRestoreIcon,
  ChevronDownIcon,
  CirclePauseIcon,
  CirclePlayIcon,
  ShieldAlertIcon,
  TrashIcon,
} from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../components/ui/button";
import { ConfirmationModal } from "../../../components/ui/confirmation-modal";
import { DeleteDialog } from "../../../components/ui/delete-dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "../../../components/ui/dropdown-menu";
import { sdkIdentityQuery } from "../../../lib/queries/workspaces";
import { toast } from "../../../lib/toast";
import { getWorkflowApiErrorMessage, workflowKeys, workflowsApi } from "../api";
import { useWorkflowBuilder } from "../hooks/use-workflow-builder";
import { WORKFLOW_CLIENT_EVENTS, summarizeValidationProblems, trackWorkflowEvent } from "../lib/analytics";
import { getWorkflowStatusBadge } from "../lib/display";
import { getNodeRegistryEntry } from "../lib/node-registry";
import { getProtectedAttributeWrites } from "../lib/protected-attributes";
import {
  hasWorkflowSaveFailedAtom,
  workflowAtom,
  workflowDefinitionAtom,
  workflowValidationProblemsAtom,
  workflowValidityAtom,
} from "../state/editor";
import { WorkflowAutoSaveIndicator } from "./workflow-auto-save-indicator";

/** forma2's WorkflowHeaderCta: the save state and, on the editor tab, the status menu with the transitions the current status allows. */
export const WorkflowHeaderCta = ({
  workflowId,
  isReadOnly,
  isEditTab,
}: Readonly<{ workflowId: string; isReadOnly: boolean; isEditTab: boolean }>) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const workflow = useAtomValue(workflowAtom);
  const validity = useAtomValue(workflowValidityAtom);
  const validationProblems = useAtomValue(workflowValidationProblemsAtom);
  const hasSaveFailed = useAtomValue(hasWorkflowSaveFailedAtom);
  const builder = useWorkflowBuilder({ workflowId, isReadOnly, loadOnMount: false });
  const [isArchiveModalOpen, setIsArchiveModalOpen] = useState(false);
  const [isDeleteDialogOpen, setIsDeleteDialogOpen] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [isProtectedWriteModalOpen, setIsProtectedWriteModalOpen] = useState(false);
  const definition = useAtomValue(workflowDefinitionAtom);
  const workspaceId = workflow?.workspaceId ?? "";
  const identity = useQuery({ ...sdkIdentityQuery(workspaceId), enabled: workspaceId !== "" && isEditTab });
  const protectedWrites = getProtectedAttributeWrites(
    definition,
    identity.data?.protectedAttributeKeys ?? []
  );

  if (!workflow) return null;
  // The runs tab is read-only; the exception is an unresolved save failure, since the unmount flush can fail during the very tab switch that lands the user there and the editor is no longer mounted to report it.
  if (!isEditTab && !hasSaveFailed) return null;

  const isArchived = workflow.status === "archived";
  const isActive = workflow.status === "enabled";
  const isBusy = isReadOnly || builder.isTransitioning || isDeleting;

  // Enabling a workflow that writes SDK-protected attributes from the respondent's answers is confirmed first; the server accepts it either way.
  const handleEnable = () => {
    if (protectedWrites.length > 0) {
      setIsProtectedWriteModalOpen(true);
      return;
    }
    void builder.enable();
  };

  const handleProtectedWriteConfirm = async () => {
    await builder.enable();
    setIsProtectedWriteModalOpen(false);
  };

  const handleArchiveConfirm = async () => {
    await builder.archive();
    setIsArchiveModalOpen(false);
  };

  // Opening the status menu of a draft whose Enable is greyed out is the closest thing to "tried to enable and could not"; the problem codes say what stopped them.
  const handleStatusMenuOpenChange = (open: boolean) => {
    if (open && !isArchived && !isActive && !validity.isReady) {
      trackWorkflowEvent(
        WORKFLOW_CLIENT_EVENTS.enableBlocked,
        summarizeValidationProblems(validationProblems)
      );
    }
  };

  const handleDelete = async () => {
    setIsDeleting(true);
    try {
      await workflowsApi.remove(workflow.id);
      // The list the user is sent to must not show the deleted workflow from its cache.
      await queryClient.invalidateQueries({ queryKey: workflowKeys.lists() });
      toast.success(t("workspace.workflows.delete_success"));
      await navigate({ href: `/workspaces/${workflow.workspaceId}/workflows` });
    } catch (error) {
      toast.error(getWorkflowApiErrorMessage(error, t("workspace.workflows.delete_failed")));
      setIsDeleting(false);
      setIsDeleteDialogOpen(false);
    }
  };

  return (
    <div className="flex items-center gap-3">
      {/* While the definition cannot change the editor is effectively read-only; an outstanding save failure still wins, since name edits keep autosaving on an enabled workflow. */}
      {builder.canEditDefinition || hasSaveFailed ? (
        <WorkflowAutoSaveIndicator />
      ) : (
        <span className="inline-flex cursor-default items-center rounded-full border border-slate-200 bg-slate-100 px-2.5 py-0.5 text-xs font-medium text-slate-600">
          {t("workspace.workflows.read_only")}
        </span>
      )}
      {isEditTab && (
        <>
          <DropdownMenu onOpenChange={handleStatusMenuOpenChange}>
            <DropdownMenuTrigger asChild disabled={isBusy}>
              <Button size="sm" loading={builder.isTransitioning} disabled={isBusy}>
                {getWorkflowStatusBadge(workflow.status, t).label}
                <ChevronDownIcon />
              </Button>
            </DropdownMenuTrigger>
            {!isReadOnly && (
              <DropdownMenuContent align="end" className="w-52">
                {isArchived ? (
                  <>
                    <DropdownMenuItem
                      icon={<ArchiveRestoreIcon className="size-4" />}
                      onSelect={() => void builder.unarchive()}>
                      {t("common.unarchive")}
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      icon={<TrashIcon className="size-4" />}
                      className="text-red-600 focus:text-red-600"
                      onSelect={() => setIsDeleteDialogOpen(true)}>
                      {t("common.delete")}
                    </DropdownMenuItem>
                  </>
                ) : (
                  <>
                    {isActive ? (
                      <DropdownMenuItem
                        icon={<CirclePauseIcon className="size-4" />}
                        onSelect={() => void builder.disable()}>
                        {t("common.disable")}
                      </DropdownMenuItem>
                    ) : (
                      // Enabling needs a workflow the server would accept; the canvas's validation state says what is missing.
                      <DropdownMenuItem
                        icon={<CirclePlayIcon className="size-4" />}
                        disabled={!validity.isReady}
                        onSelect={handleEnable}>
                        {t("common.enable")}
                      </DropdownMenuItem>
                    )}
                    <DropdownMenuItem
                      icon={<ArchiveIcon className="size-4" />}
                      onSelect={() => setIsArchiveModalOpen(true)}>
                      {t("common.archive")}
                    </DropdownMenuItem>
                  </>
                )}
              </DropdownMenuContent>
            )}
          </DropdownMenu>

          <ConfirmationModal
            open={isArchiveModalOpen}
            setOpen={setIsArchiveModalOpen}
            title={t("workspace.workflows.archive_confirm_title")}
            body={t("workspace.workflows.archive_confirm_body")}
            buttonText={t("common.archive")}
            buttonVariant="destructive"
            buttonLoading={builder.isTransitioning}
            isButtonDisabled={isReadOnly || builder.isTransitioning}
            onConfirm={() => void handleArchiveConfirm()}
            Icon={ArchiveIcon}
          />
          <ConfirmationModal
            open={isProtectedWriteModalOpen}
            setOpen={setIsProtectedWriteModalOpen}
            title={t("workspace.workflows.enable_protected_attribute_confirm_title")}
            description={t("workspace.workflows.protected_attribute_confirm_description")}
            body={
              <div className="flex flex-col gap-2">
                <p>{t("workspace.workflows.enable_protected_attribute_confirm_body")}</p>
                <ul className="list-disc pl-5">
                  {protectedWrites.map((node) => (
                    <li key={node.id}>
                      {t("workspace.workflows.enable_protected_attribute_confirm_step", {
                        step: getNodeRegistryEntry(node).title(node, t),
                        key: node.config.attributeKey,
                      })}
                    </li>
                  ))}
                </ul>
              </div>
            }
            buttonText={t("workspace.workflows.enable_protected_attribute_confirm_button")}
            buttonLoading={builder.isTransitioning}
            isButtonDisabled={isReadOnly || builder.isTransitioning}
            onConfirm={() => void handleProtectedWriteConfirm()}
            Icon={ShieldAlertIcon}
          />
          <DeleteDialog
            open={isDeleteDialogOpen}
            setOpen={setIsDeleteDialogOpen}
            deleteWhat={workflow.name}
            onDelete={() => void handleDelete()}
            isDeleting={isDeleting}
            text={t("workspace.workflows.delete_workflow_confirmation", { name: workflow.name })}
          />
        </>
      )}
    </div>
  );
};
