import { PencilIcon } from "lucide-react";
import { type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../../../components/ui/dialog";
import { FormError } from "../../../components/ui/form-error";
import { Input } from "../../../components/ui/input";
import { Label } from "../../../components/ui/label";
import { toast } from "../../../lib/toast";
import { v3ErrorMessage } from "../api";

/** The rename rule of the PATCH route (forma2's ZV3SurveyName): the trimmed name, or null when nothing is left. */
export const validSurveyName = (name: string): string | null => {
  const trimmed = name.trim();
  return trimmed.length > 0 ? trimmed : null;
};

interface RenameProps {
  surveyId: string;
  surveyName: string;
  renameSurvey: (surveyId: string, name: string) => Promise<unknown>;
}

/** forma2's RenameSurveyModal: a trimmed, non-empty name; an unchanged name just closes. */
export const RenameSurveyModal = ({
  open,
  setOpen,
  ...props
}: RenameProps & { open: boolean; setOpen: (open: boolean) => void }) => {
  const { t } = useTranslation();
  const [submitting, setSubmitting] = useState(false);
  // Held open until the rename settles: closing mid-flight is the only way to get two renames in flight on one list, where the first failure's rollback would undo the second.
  const onOpenChange = (next: boolean) => {
    if (!submitting) setOpen(next);
  };
  return (
    <Dialog open={open} onOpenChange={onOpenChange} width="narrow">
      <DialogHeader>
        <PencilIcon />
        <DialogTitle>{t("workspace.surveys.rename_survey")}</DialogTitle>
        <DialogDescription>{t("workspace.surveys.rename_survey_description")}</DialogDescription>
      </DialogHeader>
      {/* Rendered only while open, so the field starts from the survey's name on every opening and never re-seeds while the user types, not even when a failed rename rolls the row back. */}
      <RenameForm
        {...props}
        submitting={submitting}
        setSubmitting={setSubmitting}
        close={() => setOpen(false)}
      />
    </Dialog>
  );
};

const RenameForm = ({
  surveyId,
  surveyName,
  renameSurvey,
  submitting,
  setSubmitting,
  close,
}: RenameProps & { submitting: boolean; setSubmitting: (value: boolean) => void; close: () => void }) => {
  const { t } = useTranslation();
  const [name, setName] = useState(surveyName);
  const [invalid, setInvalid] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const next = validSurveyName(name);
    if (next === null) {
      setInvalid(true);
      return;
    }
    if (next === surveyName) {
      close();
      return;
    }
    setSubmitting(true);
    try {
      await renameSurvey(surveyId, next);
      toast.success(t("workspace.surveys.survey_renamed_successfully"));
      close();
    } catch (error) {
      toast.error(v3ErrorMessage(error, t("workspace.surveys.error_renaming_survey")));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
      <DialogBody className="space-y-2 p-1">
        <Label htmlFor={`rename-survey-${surveyId}`}>{t("common.name")}</Label>
        <Input
          id={`rename-survey-${surveyId}`}
          value={name}
          onChange={(e) => {
            setName(e.target.value);
            setInvalid(false);
          }}
          placeholder={t("workspace.surveys.rename_survey_placeholder")}
          isInvalid={invalid}
          aria-describedby={invalid ? `rename-survey-${surveyId}-error` : undefined}
        />
        <FormError id={`rename-survey-${surveyId}-error`}>
          {invalid ? t("workspace.surveys.survey_name_required") : undefined}
        </FormError>
      </DialogBody>
      <DialogFooter>
        <Button type="button" variant="secondary" onClick={close} disabled={submitting}>
          {t("common.cancel")}
        </Button>
        <Button type="submit" loading={submitting}>
          {t("common.save")}
        </Button>
      </DialogFooter>
    </form>
  );
};
