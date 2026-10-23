import { CopyIcon, LinkIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../../components/ui/dialog";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { Select } from "../../components/ui/select";
import { copyText } from "../../lib/clipboard";
import { type SurveyRef, generatePersonalLink } from "../../lib/queries/contacts";
import { toast } from "../../lib/toast";
import { personalLinkErrorMessage } from "./personal-link-errors";

/**
 * forma2's GeneratePersonalLinkModal: pick one of the workspace's published link surveys and get the contact's personal link to it, copied to the clipboard and shown to copy again.
 */
export const GeneratePersonalLinkModal = ({
  open,
  setOpen,
  contactId,
  publishedLinkSurveys,
}: {
  open: boolean;
  setOpen: (open: boolean) => void;
  contactId: string;
  publishedLinkSurveys: SurveyRef[];
}) => {
  const { t } = useTranslation();
  const [selectedSurveyId, setSelectedSurveyId] = useState("");
  const [isLoading, setIsLoading] = useState(false);
  const [generatedUrl, setGeneratedUrl] = useState("");

  // Each opening starts over.
  const [wasOpen, setWasOpen] = useState(open);
  if (open !== wasOpen) {
    setWasOpen(open);
    if (!open) {
      setSelectedSurveyId("");
      setGeneratedUrl("");
      setIsLoading(false);
    }
  }

  const handleGenerate = async () => {
    if (!selectedSurveyId) {
      toast.error(t("workspace.contacts.please_select_a_survey"));
      return;
    }
    setIsLoading(true);
    try {
      const { surveyUrl } = await generatePersonalLink(contactId, selectedSurveyId);
      setGeneratedUrl(surveyUrl);
      try {
        await navigator.clipboard.writeText(surveyUrl);
        toast.success(t("common.copied_to_clipboard"));
      } catch {
        toast.error(t("workspace.contacts.personal_link_generated_but_clipboard_failed", { url: surveyUrl }));
      }
    } catch (error) {
      toast.error(personalLinkErrorMessage(t, error));
    } finally {
      setIsLoading(false);
    }
  };

  const hasSurveys = publishedLinkSurveys.length > 0;
  const isDisabled = isLoading || !hasSurveys;

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogHeader>
        <LinkIcon className="size-4" />
        <DialogTitle>{t("workspace.contacts.generate_personal_link")}</DialogTitle>
        <DialogDescription>{t("workspace.contacts.generate_personal_link_description")}</DialogDescription>
      </DialogHeader>
      <DialogBody>
        <div className="m-1 space-y-4">
          <div className="space-y-2">
            <Label htmlFor="survey-select">{t("common.select_survey")}</Label>
            <Select
              id="survey-select"
              value={selectedSurveyId}
              disabled={isDisabled}
              onChange={(e) => setSelectedSurveyId(e.target.value)}>
              <option value="" disabled>
                {hasSurveys
                  ? t("workspace.contacts.select_a_survey")
                  : t("workspace.contacts.no_published_surveys")}
              </option>
              {publishedLinkSurveys.map((survey) => (
                <option key={survey.id} value={survey.id}>
                  {survey.name}
                </option>
              ))}
            </Select>
            {!hasSurveys && (
              <p className="text-sm text-slate-500">
                {t("workspace.contacts.no_published_link_surveys_available")}
              </p>
            )}
          </div>
          {generatedUrl && (
            <div className="space-y-2">
              <Label htmlFor="generated-url">{t("workspace.contacts.personal_survey_link")}</Label>
              <div className="flex gap-2">
                <Input
                  id="generated-url"
                  value={generatedUrl}
                  readOnly
                  className="flex-1 font-mono text-sm"
                />
                <Button variant="secondary" onClick={() => void copyText(generatedUrl, t)} className="gap-1">
                  <CopyIcon className="size-4" />
                  {t("common.copy")}
                </Button>
              </div>
            </div>
          )}
        </div>
      </DialogBody>
      <DialogFooter>
        <Button variant="secondary" onClick={() => setOpen(false)} disabled={isLoading}>
          {t("common.cancel")}
        </Button>
        <Button onClick={() => void handleGenerate()} disabled={!selectedSurveyId || isDisabled}>
          {isLoading ? t("common.saving") : t("common.generate")}
        </Button>
      </DialogFooter>
    </Dialog>
  );
};
