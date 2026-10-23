import { useQuery } from "@tanstack/react-query";
import { Loader2, MousePointerClickIcon } from "lucide-react";
import { type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "../../../components/ui/dialog";
import { Label } from "../../../components/ui/label";
import { MultiSelect } from "../../../components/ui/multi-select";
import { ApiError } from "../../../lib/api";
import { errorMessage } from "../../../lib/errors";
import { workspacesQuery } from "../../../lib/queries/workspaces";
import { toast } from "../../../lib/toast";
import { copyTargets, summarizeCopies } from "./copy-results";

interface CopySurveyProps {
  surveyId: string;
  currentWorkspaceId: string;
  organizationId: string;
  copySurvey: (input: { surveyId: string; targetWorkspaceId: string }) => Promise<unknown>;
}

/** forma2's CopySurveyModal: copies the survey into any of the organization's other workspaces the user can write to, all at once, and reports each failure on its own. */
export const CopySurveyModal = ({
  open,
  setOpen,
  ...props
}: CopySurveyProps & { open: boolean; setOpen: (open: boolean) => void }) => {
  const { t } = useTranslation();
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogHeader>
        <MousePointerClickIcon />
        <DialogTitle>{t("workspace.surveys.copy_survey")}</DialogTitle>
        <DialogDescription>{t("workspace.surveys.copy_survey_description")}</DialogDescription>
      </DialogHeader>
      {/* The dialog renders its content only while open, so every opening starts with nothing selected. */}
      <DialogBody className="p-1">
        <CopySurveyForm {...props} close={() => setOpen(false)} />
      </DialogBody>
    </Dialog>
  );
};

const CopySurveyForm = ({
  surveyId,
  currentWorkspaceId,
  organizationId,
  copySurvey,
  close,
}: CopySurveyProps & { close: () => void }) => {
  const { t } = useTranslation();
  const writable = useQuery(workspacesQuery(organizationId, "write"));
  const [selected, setSelected] = useState<string[]>([]);
  const [submitting, setSubmitting] = useState(false);
  const targets = copyTargets(writable.data ?? [], currentWorkspaceId);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (selected.length === 0) return;
    setSubmitting(true);
    const settled = await Promise.allSettled(
      selected.map((targetWorkspaceId) => copySurvey({ surveyId, targetWorkspaceId }))
    );
    setSubmitting(false);
    const { successCount, failures } = summarizeCopies(selected, settled, targets);
    if (successCount > 0) {
      if (failures.length === 0) toast.success(t("workspace.surveys.copy_survey_success"));
      else
        toast.error(
          t("workspace.surveys.copy_survey_partially_success", {
            success: successCount,
            error: failures.length,
          })
        );
    }
    for (const failure of failures) {
      const message =
        failure.error instanceof ApiError && failure.error.status < 500
          ? errorMessage(t, failure.error)
          : t("workspace.surveys.copy_survey_error");
      toast.error(`[${failure.workspaceName}] - ${message}`);
    }
    if (successCount > 0) close();
  };

  if (writable.isPending) {
    return (
      <div className="flex min-h-48 w-full items-center justify-center">
        <Loader2 className="animate-spin" aria-label={t("common.loading")} />
      </div>
    );
  }
  if (writable.isError) {
    return (
      <p role="alert" className="text-sm text-red-600">
        {errorMessage(t, writable.error)}
      </p>
    );
  }
  if (targets.length === 0) {
    return <p className="text-sm text-slate-600">{t("workspace.surveys.no_other_workspaces_available")}</p>;
  }
  return (
    <form onSubmit={submit} className="flex w-full flex-col">
      <div className="mb-2 flex-1 space-y-2">
        <Label htmlFor={`copy-survey-${surveyId}`}>
          {t("workspace.surveys.copy_survey_target_workspaces_label")}
        </Label>
        <MultiSelect
          id={`copy-survey-${surveyId}`}
          options={targets.map((w) => ({ value: w.id, label: w.name }))}
          value={selected}
          onChange={setSelected}
          placeholder={t("workspace.surveys.copy_survey_select_workspaces_placeholder")}
        />
      </div>
      <div className="flex justify-end gap-2 pt-4">
        <Button type="button" variant="secondary" onClick={close} disabled={submitting}>
          {t("common.cancel")}
        </Button>
        <Button type="submit" loading={submitting} disabled={selected.length === 0}>
          {t("workspace.surveys.copy_survey")}
        </Button>
      </div>
    </form>
  );
};
