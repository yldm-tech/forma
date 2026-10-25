import { ArrowLeftIcon, PencilIcon, SparklesIcon } from "lucide-react";
import { type KeyboardEvent, type ReactNode, type Ref, useEffect, useMemo, useRef } from "react";
import { useTranslation } from "react-i18next";
import { getLanguageLabel } from "@forma/i18n-utils/utils";
import { Alert, AlertDescription, AlertTitle } from "../../../components/ui/alert";
import { Button } from "../../../components/ui/button";
import { LoadingSpinner } from "../../../components/ui/loading-spinner";
import { MultiSelect } from "../../../components/ui/multi-select";
import { TooltipRenderer } from "../../../components/ui/tooltip";
import { AIUnavailableAlert } from "../../ai/ai-unavailable-alert";
import type { AIUnavailableReason } from "../../ai/availability";
import { AiDraftPreview } from "./ai-draft-preview";
import { useCreateSurveyWithAI } from "./use-create-survey-with-ai";
import {
  AI_SURVEY_LANGUAGES,
  AI_SURVEY_PROMPT_MAX_LENGTH,
  DEFAULT_AI_SURVEY_LANGUAGE,
  getHelperPrompts,
} from "./utils";

/**
 * forma2's CreateWithAIForm: the prompt and the languages, then the draft as it streams, then its review with "Regenerate" and "Save and continue". The host supplies only the footer's shell; the buttons depend on the state and are built here.
 */
export const CreateWithAIForm = ({
  workspaceId,
  organizationId,
  locale,
  surveyType = "link",
  isAIAvailable,
  aiUnavailableReason,
  onSuccess,
  onCancel,
  renderFooter,
  promptInputRef,
  onUnsavedWorkChange,
  onGeneratingChange,
}: {
  workspaceId: string;
  organizationId?: string;
  /** The reader's dashboard locale, which the language names are written in. */
  locale: string;
  surveyType?: "link" | "app";
  isAIAvailable: boolean;
  aiUnavailableReason?: AIUnavailableReason;
  onSuccess: (surveyId: string) => void;
  /** Absent where there is nothing to go back to, as on the onboarding page. */
  onCancel?: () => void;
  renderFooter?: (footer: ReactNode) => ReactNode;
  promptInputRef?: Ref<HTMLTextAreaElement>;
  /** Reports whether closing now would discard a generation in flight or an unopened draft. */
  onUnsavedWorkChange?: (hasUnsavedWork: boolean) => void;
  /** Reports whether a generation is in flight, so the host can word its confirmation. */
  onGeneratingChange?: (isGenerating: boolean) => void;
}) => {
  const { t } = useTranslation();
  const ai = useCreateSurveyWithAI({
    workspaceId,
    defaultLanguage: DEFAULT_AI_SURVEY_LANGUAGE,
    surveyType,
    isAIAvailable,
    onSuccess,
  });
  const stopButtonRef = useRef<HTMLButtonElement>(null);
  const draftRef = useRef<HTMLElement | null>(null);
  const isGenerating = ai.status === "generating";
  const isReviewing = ai.status === "review" || ai.status === "creating";

  // The textarea unmounts when generation starts; Stop is the only action, so focus goes there.
  useEffect(() => {
    if (isGenerating) stopButtonRef.current?.focus();
  }, [isGenerating]);

  useEffect(() => {
    onUnsavedWorkChange?.(ai.hasUnsavedWork);
  }, [ai.hasUnsavedWork, onUnsavedWorkChange]);

  useEffect(() => {
    onGeneratingChange?.(isGenerating);
  }, [isGenerating, onGeneratingChange]);

  // On completion focus lands on the draft's scroll container, not on the save button a Space press would trigger.
  useEffect(() => {
    if (ai.status === "review") draftRef.current?.focus();
  }, [ai.status]);

  const helperPrompts = useMemo(() => getHelperPrompts(t), [t]);
  const languageOptions = useMemo(
    () =>
      AI_SURVEY_LANGUAGES.map((code) => ({
        value: code,
        label: getLanguageLabel(code, locale) ?? code,
      })).sort((a, b) => a.label.localeCompare(b.label, locale)),
    [locale]
  );

  if (!isAIAvailable) {
    return (
      <AIUnavailableAlert
        title={t("workspace.surveys.ai_create.ai_survey_creation")}
        reason={aiUnavailableReason}
        organizationId={organizationId}
      />
    );
  }

  const handlePromptKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
      event.preventDefault();
      event.currentTarget.form?.requestSubmit();
    }
  };

  let footer: ReactNode;
  if (isGenerating) {
    footer = (
      <Button ref={stopButtonRef} type="button" variant="secondary" onClick={ai.handleStop}>
        {t("workspace.surveys.ai_create.stop")}
      </Button>
    );
  } else if (isReviewing) {
    footer = (
      <>
        {onCancel && (
          <Button type="button" variant="secondary" disabled={ai.isCreatingSurvey} onClick={onCancel}>
            {t("common.cancel")}
          </Button>
        )}
        <Button
          type="button"
          variant="secondary"
          disabled={ai.isCreatingSurvey}
          onClick={ai.handleRegenerate}>
          <SparklesIcon />
          {t("workspace.surveys.ai_create.regenerate")}
        </Button>
        <Button type="button" loading={ai.isCreatingSurvey} onClick={ai.handleOpenInEditor}>
          {t("workspace.surveys.ai_create.open_in_editor")}
        </Button>
      </>
    );
  } else {
    footer = (
      <>
        {onCancel && (
          <Button type="button" variant="secondary" onClick={onCancel}>
            {t("common.cancel")}
          </Button>
        )}
        {ai.hasKeptDraft && (
          <Button type="button" variant="secondary" onClick={ai.handleBackToDraft}>
            <ArrowLeftIcon />
            {t("workspace.surveys.ai_create.back_to_draft")}
          </Button>
        )}
        <Button type="submit" disabled={!ai.canCreate}>
          <SparklesIcon />
          {t("workspace.surveys.ai_create.create")}
        </Button>
      </>
    );
  }
  const editPromptLabel = t("workspace.surveys.ai_create.edit_prompt");

  return (
    <form className="flex h-full w-full flex-col space-y-4" onSubmit={ai.handleGenerate}>
      {ai.errorMessage && (
        <Alert variant="error">
          <AlertTitle>{t("common.error")}</AlertTitle>
          <AlertDescription>{ai.errorMessage}</AlertDescription>
        </Alert>
      )}

      {isGenerating || isReviewing ? (
        <>
          {/* The prompt this draft came from, not the one being typed: editing the prompt keeps the draft. */}
          <div className="flex shrink-0 items-center gap-1 rounded-md border border-slate-200 bg-slate-50 py-1 pr-1 pl-3">
            <p id="ai-prompt-echo" className="min-w-0 flex-1 truncate text-sm text-slate-700">
              <span className="sr-only">{t("workspace.surveys.ai_create.your_prompt")}: </span>
              {ai.submittedPrompt}
            </p>
            <TooltipRenderer tooltipContent={editPromptLabel}>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className="size-7 shrink-0 text-slate-500 hover:text-slate-800"
                disabled={ai.isCreatingSurvey}
                aria-label={editPromptLabel}
                aria-describedby="ai-prompt-echo"
                onClick={ai.handleEditPrompt}>
                <PencilIcon aria-hidden="true" />
              </Button>
            </TooltipRenderer>
          </div>
          <div className="flex min-h-0 flex-1 flex-col">
            <AiDraftPreview
              draft={ai.draft}
              isGenerating={isGenerating}
              className="flex-1"
              scrollContainerRef={draftRef}
            />
          </div>
          {isGenerating && (
            <p role="status" aria-live="polite" className="flex items-center gap-2 text-xs text-slate-500">
              <LoadingSpinner className="size-3.5" />
              {ai.statusMessage}
            </p>
          )}
        </>
      ) : (
        <>
          <div className="space-y-2">
            <textarea
              ref={promptInputRef}
              id="ai-survey-prompt"
              className="focus:ring-ring min-h-24 w-full resize-none rounded-md border border-slate-300 bg-white px-3 py-2 text-sm text-slate-800 placeholder:text-slate-400 focus:ring-2 focus:ring-offset-1 focus:outline-hidden"
              maxLength={AI_SURVEY_PROMPT_MAX_LENGTH}
              placeholder={t("workspace.surveys.ai_create.prompt_placeholder")}
              value={ai.prompt}
              onChange={(event) => ai.setPrompt(event.target.value)}
              onKeyDown={handlePromptKeyDown}
              aria-label={t("workspace.surveys.ai_create.prompt_label")}
            />
            <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-slate-500">
              <span>
                {t("workspace.surveys.ai_create.characters", {
                  count: ai.prompt.length,
                  max: AI_SURVEY_PROMPT_MAX_LENGTH,
                })}
              </span>
              <span>{t("workspace.surveys.ai_create.shortcut_hint")}</span>
            </div>
            {/* The first selection is the survey's default language and the one the model writes in; the others are translated in. An empty selection falls back to the default, since a survey without a language cannot be built. */}
            <div className="space-y-1">
              <label htmlFor="ai-survey-languages" className="text-sm font-medium text-slate-700">
                {t("common.language")}
              </label>
              <MultiSelect
                id="ai-survey-languages"
                options={languageOptions}
                value={ai.languages}
                onChange={(selected) =>
                  ai.setLanguages(selected.length > 0 ? selected : [DEFAULT_AI_SURVEY_LANGUAGE])
                }
                placeholder={t("common.select_language")}
              />
            </div>
          </div>
          <div className="space-y-2">
            <p className="text-sm font-medium text-slate-700">
              {t("workspace.surveys.ai_create.try_prompt")}
            </p>
            <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
              {helperPrompts.map((helper) => (
                <Button
                  key={helper.label}
                  type="button"
                  variant="secondary"
                  size="sm"
                  className="w-full min-w-0 justify-start text-left"
                  title={helper.prompt}
                  aria-label={`${helper.label}. ${helper.prompt}`}
                  onClick={() => {
                    ai.setPrompt(helper.prompt);
                    ai.clearError();
                  }}>
                  <helper.Icon className="size-3.5 shrink-0 text-slate-500" />
                  <span className="min-w-0 truncate">{helper.label}</span>
                </Button>
              ))}
            </div>
          </div>
        </>
      )}

      <div className="mt-auto">
        {renderFooter ? renderFooter(footer) : <div className="flex justify-end gap-2">{footer}</div>}
      </div>
    </form>
  );
};
