import { ChevronDownIcon, SparklesIcon } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { TSurvey } from "@forma/types/surveys/types";
import { getTextContent } from "@forma/types/surveys/validation";
import { cn } from "../../../../lib/cn";
import { ApiError } from "../../../../lib/api";
import { toast } from "../../../../lib/toast";
import {
  type AIUnavailableReason,
  getAIUnavailableMessage,
  getAIUnavailableMessageForErrorCode,
  translateSurveyFields,
} from "../../../ai/availability";
import { Button } from "../../../../components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../../../../components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "../../../../components/ui/dropdown-menu";
import { type TranslatableString } from "../lib/types";
import {
  extractTranslatableStrings,
  getProgressColor,
  getProgressTextColor,
  setTranslationAtPathMutable,
} from "../lib/utils";
import { TooltipRenderer } from "../../../../components/ui/tooltip";
import { TranslationRow } from "./translation-row";

interface ManageTranslationsModalProps {
  open: boolean;
  setOpen: (open: boolean) => void;
  localSurvey: TSurvey;
  setLocalSurvey: (survey: TSurvey) => void;
  languageCode: string;
  languageName: string;
  defaultLanguageName: string;
  workspaceId: string;
  isAIAvailable: boolean;
  aiUnavailableReason?: AIUnavailableReason;
}

export const ManageTranslationsModal = ({
  open,
  setOpen,
  localSurvey,
  setLocalSurvey,
  languageCode,
  languageName,
  defaultLanguageName,
  workspaceId,
  isAIAvailable,
  aiUnavailableReason,
}: ManageTranslationsModalProps) => {
  const { t } = useTranslation();

  const strings = useMemo(() => extractTranslatableStrings(localSurvey, t), [localSurvey, t]);

  const [draftTranslations, setDraftTranslations] = useState<Record<string, string>>({});
  const [missingFirst, setMissingFirst] = useState(false);
  const [isTranslating, setIsTranslating] = useState(false);
  const [translatingPaths, setTranslatingPaths] = useState<Set<string>>(new Set());

  // Initialize drafts when modal opens
  useEffect(() => {
    if (open) {
      const drafts: Record<string, string> = {};
      for (const s of strings) {
        drafts[s.path] = s.value[languageCode] ?? "";
      }
      setDraftTranslations(drafts);
    }
  }, [open, strings, languageCode]);

  const isDraftEmpty = useCallback(
    (s: TranslatableString) => {
      const val = draftTranslations[s.path];
      if (val === undefined || val === "") return true;
      // Rich text editors output HTML like "<p></p>" when empty — strip tags before checking
      const text = s.isRichText ? getTextContent(val) : val;
      return !text.trim();
    },
    [draftTranslations]
  );

  const progress = useMemo(() => {
    const total = strings.length;
    if (total === 0) return { translated: 0, total: 0, percentage: 100 };
    const translated = strings.filter((s) => !isDraftEmpty(s)).length;
    const percentage = Math.round((translated / total) * 100);
    return { translated, total, percentage };
  }, [isDraftEmpty, strings]);

  // Snapshot the sort order so rows don't jump while the user is typing.
  // Re-sort only when the sort mode is toggled or the underlying strings change.
  const displayStrings = useMemo(() => {
    if (!missingFirst) return strings;
    return [...strings].sort((a, b) => {
      const aEmpty = isDraftEmpty(a);
      const bEmpty = isDraftEmpty(b);
      if (aEmpty && !bEmpty) return -1;
      if (!aEmpty && bEmpty) return 1;
      return 0;
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps -- `isDraftEmpty` is intentionally excluded so rows don't re-sort on every keystroke; the order snapshots on strings/missingFirst changes
  }, [strings, missingFirst]);

  // Merge draft translations into localSurvey so that the recall dropdown
  // can see in-progress translations (e.g. translated headlines).
  const mergedSurvey = useMemo(() => {
    const clone = structuredClone(localSurvey);
    for (const s of strings) {
      const val = draftTranslations[s.path] ?? "";
      if (val) {
        setTranslationAtPathMutable(clone, s.path, languageCode, val, s.value.default);
      }
    }
    return clone;
  }, [localSurvey, strings, draftTranslations, languageCode]);

  const handleDraftChange = useCallback((path: string, value: string) => {
    setDraftTranslations((prev) => ({ ...prev, [path]: value }));
  }, []);

  // forma2's AI translation: only the fields still empty here whose default text is not.
  const emptyFields = useMemo(
    () => strings.filter((s) => isDraftEmpty(s) && (s.value.default || "").trim() !== ""),
    [isDraftEmpty, strings]
  );

  const aiErrorMessage = (errorCode: string): string => {
    const unavailable = getAIUnavailableMessageForErrorCode(errorCode, t);
    if (unavailable) return unavailable;
    if (errorCode === "ai_quota_exceeded") return t("workspace.surveys.edit.ai_translation_quota_exceeded");
    return t("workspace.surveys.edit.ai_translation_failed");
  };

  const handleTranslateWithAI = async () => {
    if (emptyFields.length === 0) return;
    setIsTranslating(true);
    setTranslatingPaths(new Set(emptyFields.map((s) => s.path)));
    const toastId = toast.loading(t("workspace.surveys.edit.ai_translating"));
    try {
      const result = await translateSurveyFields(workspaceId, {
        fields: emptyFields.map((s) => ({
          path: s.path,
          defaultText: s.value.default || "",
          isRichText: s.isRichText,
        })),
        // The names the editor shows, which is what the model is told to translate between.
        sourceLanguage: defaultLanguageName,
        targetLanguage: languageName,
      });
      setDraftTranslations((prev) => ({ ...prev, ...result.translations }));
      toast.success(t("workspace.surveys.edit.ai_translation_complete"), { id: toastId });
    } catch (error) {
      toast.error(
        error instanceof ApiError
          ? aiErrorMessage(error.code)
          : t("workspace.surveys.edit.ai_translation_failed"),
        {
          id: toastId,
        }
      );
    } finally {
      setIsTranslating(false);
      setTranslatingPaths(new Set());
    }
  };

  let aiTooltip: string | undefined;
  if (!isAIAvailable && !isTranslating) aiTooltip = getAIUnavailableMessage(aiUnavailableReason, t);
  else if (emptyFields.length === 0 && !isTranslating)
    aiTooltip = t("workspace.surveys.edit.ai_translation_all_fields_populated");

  const handleSave = () => {
    const updatedSurvey = structuredClone(localSurvey);
    for (const s of strings) {
      const draft = draftTranslations[s.path] ?? "";
      // Rich-text editors keep an empty wrapper like "<p><br></p>" in the draft. That's
      // visually empty but a non-empty string, so without normalization it would survive
      // save and downstream checks would treat the field as translated.
      const val = s.isRichText && getTextContent(draft).trim() === "" ? "" : draft;
      setTranslationAtPathMutable(updatedSurvey, s.path, languageCode, val, s.value.default);
    }
    setLocalSurvey(updatedSurvey);
    setOpen(false);
  };

  const handleCancel = () => {
    setOpen(false);
  };

  const progressColor = getProgressColor(progress.percentage);
  const progressTextColor = getProgressTextColor(progress.percentage);

  return (
    <Dialog
      open={open}
      onOpenChange={setOpen}
      width="wide"
      className="max-h-[85dvh]"
      disableCloseOnOutsideClick>
      <DialogHeader>
        <DialogTitle>{t("workspace.surveys.edit.manage_translations")}</DialogTitle>
        <div className="mt-2 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <div className="h-1.5 w-20 overflow-hidden rounded-full bg-slate-200">
              <div
                className={cn("h-full rounded-full transition-all", progressColor)}
                style={{ width: `${progress.percentage}%` }}
              />
            </div>
            <span className={cn("text-sm font-medium", progressTextColor)}>
              {progress.translated}/{progress.total}
            </span>
          </div>
          <div className="flex items-center gap-2">
            <TooltipRenderer tooltipContent={aiTooltip} shouldRender={aiTooltip !== undefined}>
              <Button
                size="sm"
                className="text-xs"
                onClick={() => void handleTranslateWithAI()}
                disabled={!isAIAvailable || isTranslating || emptyFields.length === 0}
                loading={isTranslating}>
                <SparklesIcon className="size-3.5" />
                {t("workspace.surveys.edit.ai_translate")}
              </Button>
            </TooltipRenderer>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="secondary" size="sm" className="text-xs">
                  {missingFirst
                    ? t("workspace.surveys.edit.missing_first")
                    : t("workspace.surveys.edit.show_in_order")}
                  <ChevronDownIcon className="ml-1 size-3.5" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="text-xs">
                <DropdownMenuItem
                  className={cn(!missingFirst && "font-semibold", "text-xs")}
                  onSelect={() => setMissingFirst(false)}>
                  {t("workspace.surveys.edit.show_in_order")}
                </DropdownMenuItem>
                <DropdownMenuItem
                  className={cn(missingFirst && "font-semibold", "text-xs")}
                  onSelect={() => setMissingFirst(true)}>
                  {t("workspace.surveys.edit.missing_first")}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>
      </DialogHeader>

      <DialogBody>
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b text-left text-slate-500">
              <th className="w-16 py-2 pr-2 font-medium">{t("common.id")}</th>
              <th className="w-1/3 py-2 pr-2 font-medium">{defaultLanguageName}</th>
              <th className="py-2 font-medium">{languageName}</th>
            </tr>
          </thead>
          <tbody>
            {displayStrings.map((s) => (
              <TranslationRow
                key={s.path}
                s={s}
                value={draftTranslations[s.path] ?? ""}
                onChange={handleDraftChange}
                localSurvey={mergedSurvey}
                languageCode={languageCode}
                disabled={translatingPaths.has(s.path)}
              />
            ))}
          </tbody>
        </table>
      </DialogBody>

      <DialogFooter>
        <Button variant="secondary" size="sm" onClick={handleCancel}>
          {t("common.cancel")}
        </Button>
        <Button size="sm" onClick={handleSave}>
          {t("common.save")}
        </Button>
      </DialogFooter>
    </Dialog>
  );
};
