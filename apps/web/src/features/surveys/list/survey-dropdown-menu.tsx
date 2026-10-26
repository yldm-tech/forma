import { Link, useNavigate } from "@tanstack/react-router";
import {
  ArchiveIcon,
  ArchiveRestoreIcon,
  ArrowRightLeftIcon,
  CircleAlert,
  CopyIcon,
  EyeIcon,
  LayoutTemplateIcon,
  LinkIcon,
  MoreVertical,
  PencilIcon,
  SquarePenIcon,
  TrashIcon,
} from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../components/ui/button";
import { DeleteDialog } from "../../../components/ui/delete-dialog";
import {
  Dialog,
  DialogBody,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../../../components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "../../../components/ui/dropdown-menu";
import { copyText } from "../../../lib/clipboard";
import { cn } from "../../../lib/cn";
import { errorMessage } from "../../../lib/errors";
import { toast } from "../../../lib/toast";
import { type SurveyListItem, v3ErrorMessage } from "../api";
import { SaveAsTemplateDialog } from "../templates/template-details-dialog";
import { CopySurveyModal } from "./copy-survey-modal";
import { EditCautionDialog } from "./edit-caution-dialog";
import type { SurveyStatus } from "./filters";
import { RenameSurveyModal } from "./rename-survey-modal";
import { CHANGEABLE_STATUSES, type SurveyRowRules, surveyLink, surveyPreviewLink } from "./row-rules";
import { SurveyStatusIndicator } from "./survey-status-indicator";

/** What a row's menu can do to its survey; the list owns the mutations so their optimistic patches land in its cache. */
export interface SurveyActions {
  deleteSurvey: (surveyId: string) => Promise<unknown>;
  updateSurveyStatus: (surveyId: string, status: SurveyStatus) => Promise<unknown>;
  archiveSurvey: (surveyId: string) => Promise<unknown>;
  restoreSurvey: (surveyId: string) => Promise<unknown>;
  renameSurvey: (surveyId: string, name: string) => Promise<unknown>;
  /** Resolves to the created survey, so a duplicate can be opened. */
  copySurvey: (input: { surveyId: string; targetWorkspaceId: string }) => Promise<{ id: string }>;
}

const statusLabelKey: Record<SurveyStatus, string> = {
  inProgress: "common.in_progress",
  paused: "common.paused",
  completed: "common.completed",
  draft: "common.draft",
};

/** forma2's SurveyDropDownMenu: restore and delete for an archived survey; edit, rename, duplicate, status, copy, preview, copy link, archive and delete for any other. An owner or a manager can also save it as a template of the organization. Delete, the one action that cannot be undone, comes last, set apart and in red. */
export const SurveyDropdownMenu = ({
  survey,
  rules,
  workspaceId,
  organizationId,
  publicDomain,
  actions,
  canSaveAsTemplate,
}: {
  survey: SurveyListItem;
  rules: SurveyRowRules;
  workspaceId: string;
  organizationId: string;
  publicDomain: string | undefined;
  actions: SurveyActions;
  /** The caller is an owner or a manager of the organization, who may save its surveys as templates. */
  canSaveAsTemplate: boolean;
}) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [menuOpen, setMenuOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [archiveOpen, setArchiveOpen] = useState(false);
  const [renameOpen, setRenameOpen] = useState(false);
  const [copyOpen, setCopyOpen] = useState(false);
  const [saveAsTemplateOpen, setSaveAsTemplateOpen] = useState(false);
  const [cautionOpen, setCautionOpen] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [isArchiving, setIsArchiving] = useState(false);
  const [isDuplicating, setIsDuplicating] = useState(false);
  const editHrefOf = (surveyId: string) => `/workspaces/${workspaceId}/surveys/${surveyId}/edit`;
  const editHref = editHrefOf(survey.id);
  const isInProgress = survey.status === "inProgress";

  if (!rules.hasVisibleActions) return null;

  const changeStatus = async (status: SurveyStatus) => {
    setMenuOpen(false);
    const id = toast.loading(t("workspace.surveys.status_updating"));
    try {
      await actions.updateSurveyStatus(survey.id, status);
      toast.success(t("workspace.surveys.status_updated_successfully"), { id });
    } catch (error) {
      toast.error(v3ErrorMessage(error, t("workspace.surveys.error_updating_status")), { id });
    }
  };

  const deleteSurvey = async () => {
    setIsDeleting(true);
    try {
      await actions.deleteSurvey(survey.id);
      toast.success(t("workspace.surveys.survey_deleted_successfully"));
      setDeleteOpen(false);
    } catch (error) {
      toast.error(v3ErrorMessage(error, t("workspace.surveys.error_deleting_survey")));
    } finally {
      setIsDeleting(false);
    }
  };

  /** The archive toast's Undo: the survey comes back, and one that was running (which archiving paused) runs again. */
  const undoArchive = async () => {
    const id = toast.loading(t("workspace.surveys.restoring_survey"));
    try {
      await actions.restoreSurvey(survey.id);
      if (isInProgress) await actions.updateSurveyStatus(survey.id, "inProgress");
      toast.success(t("workspace.surveys.survey_restored_successfully"), { id });
    } catch (error) {
      toast.error(v3ErrorMessage(error, t("workspace.surveys.error_restoring_survey")), { id });
    }
  };

  const archiveSurvey = async () => {
    setIsArchiving(true);
    const id = toast.loading(t("workspace.surveys.archiving_survey"));
    try {
      await actions.archiveSurvey(survey.id);
      toast.success(t("workspace.surveys.survey_archived_successfully"), {
        id,
        action: { label: t("workspace.surveys.undo_archive"), onClick: () => void undoArchive() },
      });
      setArchiveOpen(false);
    } catch (error) {
      toast.error(v3ErrorMessage(error, t("workspace.surveys.error_archiving_survey")), { id });
    } finally {
      setIsArchiving(false);
    }
  };

  const restoreSurvey = async () => {
    setMenuOpen(false);
    const id = toast.loading(t("workspace.surveys.restoring_survey"));
    try {
      await actions.restoreSurvey(survey.id);
      toast.success(t("workspace.surveys.survey_restored_successfully"), { id });
    } catch (error) {
      toast.error(v3ErrorMessage(error, t("workspace.surveys.error_restoring_survey")), { id });
    }
  };

  /** Duplicates the survey into this workspace. The copy lands wherever the sort puts it, so the toast offers to open it; openCopy opens its editor at once instead. */
  const duplicateSurvey = async ({ openCopy = false }: { openCopy?: boolean } = {}) => {
    setMenuOpen(false);
    setIsDuplicating(true);
    try {
      const copy = await actions.copySurvey({ surveyId: survey.id, targetWorkspaceId: workspaceId });
      if (openCopy) {
        toast.success(t("workspace.surveys.survey_duplicated_successfully"));
        void navigate({ href: editHrefOf(copy.id) });
      } else {
        toast.success(t("workspace.surveys.survey_duplicated_successfully"), {
          action: {
            label: t("workspace.surveys.open_duplicate"),
            onClick: () => void navigate({ href: editHrefOf(copy.id) }),
          },
        });
      }
    } catch (error) {
      toast.error(errorMessage(t, error));
    } finally {
      setIsDuplicating(false);
    }
  };

  const openDialog = (open: (value: boolean) => void) => (event: Event) => {
    event.preventDefault();
    setMenuOpen(false);
    open(true);
  };

  return (
    <div>
      <DropdownMenu open={menuOpen} onOpenChange={setMenuOpen}>
        <DropdownMenuTrigger asChild disabled={rules.isMenuDisabled}>
          <button
            type="button"
            aria-label={t("workspace.surveys.open_options_for", { name: survey.name })}
            className={cn(
              "rounded-lg border bg-white p-2",
              rules.isMenuDisabled ? "cursor-not-allowed opacity-50" : "cursor-pointer hover:bg-slate-50"
            )}>
            <MoreVertical className="size-4" aria-hidden="true" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="inline-block w-auto min-w-max">
          <DropdownMenuGroup>
            {rules.isArchived && rules.canManage && (
              <>
                <DropdownMenuItem
                  icon={<ArchiveRestoreIcon className="size-4" />}
                  onSelect={(event) => {
                    event.preventDefault();
                    void restoreSurvey();
                  }}>
                  {t("workspace.surveys.restore")}
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem
                  className="text-red-600 focus:text-red-600"
                  icon={<TrashIcon className="size-4" />}
                  onSelect={openDialog(setDeleteOpen)}>
                  {t("common.delete")}
                </DropdownMenuItem>
              </>
            )}
            {!rules.isArchived && rules.canManage && (
              <>
                <DropdownMenuItem asChild>
                  <Link
                    to={editHref}
                    className="flex w-full items-center gap-x-2"
                    onClick={(event) => {
                      if (survey.responseCount === 0) return;
                      event.preventDefault();
                      setMenuOpen(false);
                      setCautionOpen(true);
                    }}>
                    <SquarePenIcon className="size-4" />
                    {t("common.edit")}
                  </Link>
                </DropdownMenuItem>
                <DropdownMenuItem
                  icon={<PencilIcon className="size-4" />}
                  onSelect={openDialog(setRenameOpen)}>
                  {t("common.rename")}
                </DropdownMenuItem>
                <DropdownMenuItem
                  icon={<CopyIcon className="size-4" />}
                  disabled={isDuplicating}
                  onSelect={(event) => {
                    event.preventDefault();
                    void duplicateSurvey();
                  }}>
                  {t("common.duplicate")}
                </DropdownMenuItem>
              </>
            )}
            {rules.canChangeStatus && (
              <DropdownMenuSub>
                <DropdownMenuSubTrigger>{t("workspace.surveys.change_status")}</DropdownMenuSubTrigger>
                <DropdownMenuSubContent sideOffset={8}>
                  <DropdownMenuRadioGroup
                    value={survey.status}
                    onValueChange={(value) => void changeStatus(value as SurveyStatus)}>
                    {CHANGEABLE_STATUSES.map((status) => (
                      <DropdownMenuRadioItem
                        key={status}
                        value={status}
                        // The menu is closed by changeStatus, so the loading toast plays in the page.
                        onSelect={(event) => event.preventDefault()}>
                        <span className="flex items-center gap-2">
                          <SurveyStatusIndicator status={status} />
                          {t(statusLabelKey[status])}
                        </span>
                      </DropdownMenuRadioItem>
                    ))}
                  </DropdownMenuRadioGroup>
                </DropdownMenuSubContent>
              </DropdownMenuSub>
            )}
            {!rules.isArchived && rules.canManage && (
              <DropdownMenuItem
                icon={<ArrowRightLeftIcon className="size-4" />}
                onSelect={openDialog(setCopyOpen)}>
                {t("workspace.surveys.copy_to")}
              </DropdownMenuItem>
            )}
            {!rules.isArchived && rules.canManage && canSaveAsTemplate && (
              <DropdownMenuItem
                icon={<LayoutTemplateIcon className="size-4" />}
                onSelect={openDialog(setSaveAsTemplateOpen)}>
                {t("workspace.surveys.organization_templates.save_as_template")}
              </DropdownMenuItem>
            )}
            {rules.canPreviewOrCopyLink && publicDomain && (
              <>
                <DropdownMenuItem
                  icon={<EyeIcon className="size-4" />}
                  onSelect={() => globalThis.open(surveyPreviewLink(publicDomain, survey.id), "_blank")}>
                  {t("common.preview")}
                </DropdownMenuItem>
                <DropdownMenuItem
                  icon={<LinkIcon className="size-4" />}
                  onSelect={() => void copyText(surveyLink(publicDomain, survey.id), t)}>
                  {t("common.copy_link")}
                </DropdownMenuItem>
              </>
            )}
            {!rules.isArchived && rules.canManage && (
              <>
                <DropdownMenuItem
                  icon={<ArchiveIcon className="size-4" />}
                  onSelect={openDialog(setArchiveOpen)}>
                  {t("workspace.surveys.archive")}
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem
                  className="text-red-600 focus:text-red-600"
                  icon={<TrashIcon className="size-4" />}
                  onSelect={openDialog(setDeleteOpen)}>
                  {t("common.delete")}
                </DropdownMenuItem>
              </>
            )}
          </DropdownMenuGroup>
        </DropdownMenuContent>
      </DropdownMenu>

      {rules.canManage && (
        <DeleteDialog
          deleteWhat={t("common.survey")}
          open={deleteOpen}
          setOpen={setDeleteOpen}
          onDelete={() => void deleteSurvey()}
          text={t("workspace.surveys.delete_survey_and_responses_warning")}
          isDeleting={isDeleting}>
          <p className="mt-2 break-words text-slate-900">
            {t("workspace.surveys.delete_survey_summary", { name: survey.name, count: survey.responseCount })}
          </p>
        </DeleteDialog>
      )}
      {!rules.isArchived && rules.canManage && (
        <>
          <ArchiveSurveyDialog
            surveyName={survey.name}
            responseCount={survey.responseCount}
            open={archiveOpen}
            setOpen={setArchiveOpen}
            isInProgress={isInProgress}
            isArchiving={isArchiving}
            onConfirm={() => void archiveSurvey()}
          />
          <RenameSurveyModal
            open={renameOpen}
            setOpen={setRenameOpen}
            surveyId={survey.id}
            surveyName={survey.name}
            renameSurvey={actions.renameSurvey}
          />
          <CopySurveyModal
            open={copyOpen}
            setOpen={setCopyOpen}
            surveyId={survey.id}
            currentWorkspaceId={workspaceId}
            organizationId={organizationId}
            copySurvey={actions.copySurvey}
          />
          {canSaveAsTemplate && (
            <SaveAsTemplateDialog
              open={saveAsTemplateOpen}
              setOpen={setSaveAsTemplateOpen}
              surveyId={survey.id}
              surveyName={survey.name}
              workspaceId={workspaceId}
              organizationId={organizationId}
            />
          )}
          {survey.responseCount > 0 && (
            <EditCautionDialog
              open={cautionOpen}
              setOpen={setCautionOpen}
              onEdit={() => void navigate({ href: editHref })}
              onDuplicate={() => duplicateSurvey({ openCopy: true })}
              isDuplicating={isDuplicating}
            />
          )}
        </>
      )}
    </div>
  );
};

/** forma2's archive confirmation: archiving a running survey stops it, so that confirmation cannot be dismissed by accident and its button says so. */
const ArchiveSurveyDialog = ({
  surveyName,
  responseCount,
  open,
  setOpen,
  isInProgress,
  isArchiving,
  onConfirm,
}: {
  surveyName: string;
  responseCount: number;
  open: boolean;
  setOpen: (open: boolean) => void;
  isInProgress: boolean;
  isArchiving: boolean;
  onConfirm: () => void;
}) => {
  const { t } = useTranslation();
  return (
    <Dialog
      open={open}
      onOpenChange={setOpen}
      width="narrow"
      hideCloseButton={isInProgress}
      disableCloseOnOutsideClick={isInProgress}>
      <DialogHeader>
        <div className="flex gap-2">
          <CircleAlert className="mt-0.5 size-4 shrink-0 text-slate-500" />
          <div className="flex flex-col gap-1">
            <DialogTitle>{t("workspace.surveys.archive_survey")}</DialogTitle>
            <DialogDescription>
              {isInProgress
                ? t("workspace.surveys.archive_survey_description_in_progress")
                : t("workspace.surveys.archive_survey_description")}
            </DialogDescription>
          </div>
        </div>
      </DialogHeader>
      <DialogBody>
        <p className="break-words text-slate-900">
          {t("workspace.surveys.archive_survey_summary", { name: surveyName, count: responseCount })}
        </p>
        <div className="mt-2 whitespace-pre-line">{t("workspace.surveys.archive_survey_warning")}</div>
      </DialogBody>
      <DialogFooter>
        <Button variant="ghost" onClick={() => setOpen(false)}>
          {t("common.cancel")}
        </Button>
        <Button variant={isInProgress ? "destructive" : "default"} loading={isArchiving} onClick={onConfirm}>
          {isInProgress ? t("workspace.surveys.stop_and_archive") : t("common.archive")}
        </Button>
      </DialogFooter>
    </Dialog>
  );
};
