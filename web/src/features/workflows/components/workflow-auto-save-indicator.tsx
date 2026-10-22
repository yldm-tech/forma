import { useAtomValue } from "jotai";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { TooltipRenderer } from "../../../components/ui/tooltip";
import { cn } from "../../../lib/cn";
import {
  hasWorkflowSaveFailedAtom,
  isWorkflowDirtyAtom,
  isWorkflowSavingAtom,
  workflowLastSavedAtAtom,
  workflowSaveErrorAtom,
  workflowValidityAtom,
} from "../state/editor";

const SAVED_FLASH_MS = 3000;

const PILL_CLASSES = {
  failed: "border-red-200 bg-red-100 text-red-800",
  blocked: "border-amber-200 bg-amber-100 text-amber-800",
  saved: "border-green-600 bg-green-50 text-green-800",
  idle: "border-slate-200 bg-slate-100 text-slate-600",
} as const;

/** forma2's WorkflowAutoSaveIndicator: the draft's actual state. "Saving" from the first keystroke until the debounced PATCH lands, then "All changes saved" with a short green flash; red for as long as a save is outstanding, since autosaves never toast and this pill is the only report; amber while the name is empty and nothing can be sent. */
export const WorkflowAutoSaveIndicator = () => {
  const { t } = useTranslation();
  const lastSavedAt = useAtomValue(workflowLastSavedAtAtom);
  const hasFailed = useAtomValue(hasWorkflowSaveFailedAtom);
  const saveError = useAtomValue(workflowSaveErrorAtom);
  const isDirty = useAtomValue(isWorkflowDirtyAtom);
  const isSaving = useAtomValue(isWorkflowSavingAtom);
  const { isNameValid } = useAtomValue(workflowValidityAtom);
  // The save whose flash has run out; the flash shows while the last save is newer than it.
  const [expiredSavedAt, setExpiredSavedAt] = useState<number | null>(null);
  const showSaved = lastSavedAt !== null && lastSavedAt !== expiredSavedAt;

  // A dirty draft without a name is never sent, so it must not claim a save is on its way.
  const isBlocked = isDirty && !isNameValid;

  useEffect(() => {
    if (!lastSavedAt) return;
    const timer = setTimeout(() => setExpiredSavedAt(lastSavedAt), SAVED_FLASH_MS);
    return () => clearTimeout(timer);
  }, [lastSavedAt]);

  let state: keyof typeof PILL_CLASSES = "idle";
  let label = t("workspace.workflows.all_changes_saved");
  if (hasFailed) {
    state = "failed";
    label = t("workspace.workflows.autosave_failed");
  } else if (isBlocked) {
    state = "blocked";
    label = t("workspace.workflows.autosave_blocked");
  } else if (isDirty || isSaving) {
    label = t("workspace.workflows.saving_changes");
  } else if (showSaved) {
    state = "saved";
  }

  // A refused draft has a reason worth quoting; an unreachable one does not.
  let tooltipContent = t("workspace.workflows.autosave_failed_tooltip");
  if (isBlocked) {
    tooltipContent = t("workspace.workflows.autosave_blocked_tooltip");
  } else if (saveError?.detail) {
    tooltipContent = t("workspace.workflows.autosave_failed_tooltip_rejected", { detail: saveError.detail });
  }
  const hasTooltip = hasFailed || isBlocked;

  return (
    <TooltipRenderer
      shouldRender={hasTooltip}
      tooltipContent={tooltipContent}
      className="max-w-64 text-center">
      {/* A polite live region, since this pill is the whole report; the reason rides along for screen readers, the tooltip being hover-only. */}
      <span
        role="status"
        aria-live="polite"
        className={cn(
          "inline-flex cursor-default items-center rounded-full border px-2.5 py-0.5 text-xs font-medium transition-colors duration-300",
          PILL_CLASSES[state]
        )}>
        {label}
        {hasTooltip ? <span className="sr-only">. {tooltipContent}</span> : null}
      </span>
    </TooltipRenderer>
  );
};
