import { Link, useNavigate } from "@tanstack/react-router";
import {
  ArchiveIcon,
  ArchiveRestoreIcon,
  ArrowRightLeftIcon,
  CircleAlert,
  CopyIcon,
  EyeIcon,
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
  copySurvey: (input: { surveyId: string; targetWorkspaceId: string }) => Promise<unknown>;
}

const statusLabelKey: Record<SurveyStatus, string> = {
  inProgress: "common.in_progress",
  paused: "common.paused",
  completed: "common.completed",
  draft: "common.draft",
};

/** forma2's SurveyDropDownMenu: restore and delete for an archived survey; edit, rename, duplicate, status, copy, preview, copy link, delete and archive for any other. */
export const SurveyDropdownMenu = ({
  survey,
  rules,
  workspaceId,
  organizationId,
  publicDomain,
  actions,
}: {
  survey: SurveyListItem;
  rules: SurveyRowRules;
  workspaceId: string;
  organizationId: string;
  publicDomain: string | undefined;
  actions: SurveyActions;
}) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [menuOpen, setMenuOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [archiveOpen, setArchiveOpen] = useState(false);
  const [renameOpen, setRenameOpen] = useState(false);
  const [copyOpen, setCopyOpen] = useState(false);
  const [cautionOpen, setCautionOpen] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [isArchiving, setIsArchiving] = useState(false);
  const [isDuplicating, setIsDuplicating] = useState(false);
  const editHref = `/workspaces/${workspaceId}/surveys/${survey.id}/edit`;
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

  const archiveSurvey = async () => {
    setIsArchiving(true);
    const id = toast.loading(t("workspace.surveys.archiving_survey"));
    try {
      await actions.archiveSurvey(survey.id);
      toast.success(t("workspace.surveys.survey_archived_successfully"), { id });
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

  const duplicateSurvey = async () => {
    setMenuOpen(false);
    setIsDuplicating(true);
    try {
      await actions.copySurvey({ surveyId: survey.id, targetWorkspaceId: workspaceId });
      toast.success(t("workspace.surveys.survey_duplicated_successfully"));
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
            aria-label={t("workspace.surveys.open_options")}
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
                  icon={<TrashIcon className="size-4" />}
                  onSelect={openDialog(setDeleteOpen)}>
                  {t("common.delete")}
                </DropdownMenuItem>
                <DropdownMenuItem
                  className="text-red-600 focus:text-red-600"
                  icon={<ArchiveIcon className="size-4" />}
                  onSelect={openDialog(setArchiveOpen)}>
                  {t("workspace.surveys.archive")}
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
          isDeleting={isDeleting}
        />
      )}
      {!rules.isArchived && rules.canManage && (
        <>
          <ArchiveSurveyDialog
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
          {survey.responseCount > 0 && (
            <EditCautionDialog
              open={cautionOpen}
              setOpen={setCautionOpen}
              onEdit={() => void navigate({ href: editHref })}
            />
          )}
        </>
      )}
    </div>
  );
};

/** forma2's archive confirmation: archiving a running survey stops it, so that confirmation cannot be dismissed by accident and its button says so. */
const ArchiveSurveyDialog = ({
  open,
  setOpen,
  isInProgress,
  isArchiving,
  onConfirm,
}: {
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
        <div className="whitespace-pre-line">{t("workspace.surveys.archive_survey_warning")}</div>
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
