import { useNavigate } from "@tanstack/react-router";
import { SparklesIcon } from "lucide-react";
import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "../../../lib/cn";
import { ConfirmationModal } from "../../../components/ui/confirmation-modal";
import {
  Dialog,
  DialogBody,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../../../components/ui/dialog";
import type { AIUnavailableReason } from "../../ai/availability";
import { surveyEditorPath } from "../templates/use-create-survey";
import { CreateWithAIForm } from "./create-with-ai-form";

/**
 * forma2's CreateWithAIDialog: the create-with-AI form in a modal of one fixed size. Closing it while a generation runs or an unopened draft exists asks first, and a saved draft opens in the editor.
 */
export const CreateWithAIDialog = ({
  open,
  onOpenChange,
  workspaceId,
  organizationId,
  locale,
  surveyType,
  isAIAvailable,
  aiUnavailableReason,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  workspaceId: string;
  organizationId?: string;
  locale: string;
  surveyType: "link" | "app";
  isAIAvailable: boolean;
  aiUnavailableReason?: AIUnavailableReason;
}) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [hasUnsavedWork, setHasUnsavedWork] = useState(false);
  const [isGenerating, setIsGenerating] = useState(false);
  const [isConfirmingDiscard, setIsConfirmingDiscard] = useState(false);

  const setDialogOpen = (next: boolean) => {
    // Reloading the page already warns; closing the dialog throws away the same work, so it asks too.
    if (!next && hasUnsavedWork) {
      setIsConfirmingDiscard(true);
      return;
    }
    onOpenChange(next);
  };

  // The prompt takes focus whenever it mounts: on opening, and on going back from a draft to edit it.
  const focusPrompt = useCallback((node: HTMLTextAreaElement | null) => node?.focus(), []);

  // Read as plain calls so the keys stay visible to the translation key check.
  const discardGenerationTitle = t("workspace.surveys.ai_create.discard_generation_title");
  const discardDraftTitle = t("workspace.surveys.ai_create.discard_draft_title");
  const discardGenerationBody = t("workspace.surveys.ai_create.discard_generation_body");
  const discardDraftBody = t("workspace.surveys.ai_create.discard_draft_body");

  return (
    <>
      {/* A stray click outside must not kill a generation the user has been watching. */}
      <Dialog open={open} onOpenChange={setDialogOpen} disableCloseOnOutsideClick>
        <DialogHeader>
          <SparklesIcon aria-hidden="true" />
          <DialogTitle>{t("workspace.surveys.ai_create.dialog_title")}</DialogTitle>
          <DialogDescription>{t("workspace.surveys.ai_create.dialog_description")}</DialogDescription>
        </DialogHeader>
        {/* One fixed height for every state, so the modal never moves as the draft streams in; without AI the body is one alert. */}
        <DialogBody className={cn("-mx-1 flex flex-none flex-col px-1 pb-1", isAIAvailable && "h-[26rem]")}>
          <CreateWithAIForm
            workspaceId={workspaceId}
            organizationId={organizationId}
            locale={locale}
            surveyType={surveyType}
            isAIAvailable={isAIAvailable}
            aiUnavailableReason={aiUnavailableReason}
            onSuccess={(surveyId) => void navigate({ href: surveyEditorPath(workspaceId, surveyId) })}
            promptInputRef={focusPrompt}
            onUnsavedWorkChange={setHasUnsavedWork}
            onGeneratingChange={setIsGenerating}
            onCancel={() => setDialogOpen(false)}
            renderFooter={(footer) => <DialogFooter>{footer}</DialogFooter>}
          />
        </DialogBody>
      </Dialog>
      <ConfirmationModal
        open={isConfirmingDiscard}
        setOpen={setIsConfirmingDiscard}
        title={isGenerating ? discardGenerationTitle : discardDraftTitle}
        description={isGenerating ? discardGenerationBody : discardDraftBody}
        body={null}
        buttonText={t("workspace.surveys.ai_create.discard")}
        buttonVariant="destructive"
        cancelButtonText={t("workspace.surveys.ai_create.keep_editing")}
        onConfirm={() => {
          setIsConfirmingDiscard(false);
          setHasUnsavedWork(false);
          onOpenChange(false);
        }}
      />
    </>
  );
};
