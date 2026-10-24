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

/** forma2's EditPublicSurveyAlertDialog, shown before opening the editor of a survey that already has responses. */
export const EditCautionDialog = ({
  open,
  setOpen,
  onEdit,
}: {
  open: boolean;
  setOpen: (open: boolean) => void;
  onEdit: () => void;
}) => {
  const { t } = useTranslation();
  return (
    <Dialog open={open} onOpenChange={setOpen} width="narrow">
      <DialogHeader>
        <DialogTitle>{t("workspace.surveys.edit.caution_edit_published_survey")}</DialogTitle>
        <DialogDescription>{t("workspace.surveys.edit.caution_recommendation")}</DialogDescription>
      </DialogHeader>
      <DialogBody>
        <p>{t("workspace.surveys.edit.caution_explanation_intro")}</p>
        <ul className="mt-3 list-disc space-y-0.5 pl-5">
          <li>{t("workspace.surveys.edit.caution_explanation_responses_are_safe")}</li>
          <li>{t("workspace.surveys.edit.caution_explanation_new_responses_separated")}</li>
          <li>{t("workspace.surveys.edit.caution_explanation_only_new_responses_in_summary")}</li>
        </ul>
      </DialogBody>
      <DialogFooter>
        <Button variant="secondary" onClick={() => setOpen(false)}>
          {t("common.cancel")}
        </Button>
        <Button
          onClick={() => {
            setOpen(false);
            onEdit();
          }}>
          {t("common.edit")}
        </Button>
      </DialogFooter>
    </Dialog>
  );
};
