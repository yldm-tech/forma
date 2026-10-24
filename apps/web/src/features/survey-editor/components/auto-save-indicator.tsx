import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "../../../lib/cn";
import { Button } from "../../../components/ui/button";
import { TooltipRenderer } from "../../../components/ui/tooltip";

/** Where the draft's autosave stands: nothing saved yet this session, a save under way, the last success, or failures in a row (shown once the editor stops retrying quietly). */
export type AutoSaveStatus =
  | { state: "idle" }
  | { state: "saving" }
  | { state: "saved"; at: Date }
  | { state: "failed"; failures: number };

interface AutoSaveIndicatorProps {
  isDraft: boolean;
  status: AutoSaveStatus;
  /** The user's locale, for the time of the last save. */
  locale: string;
  onRetry: () => void;
}

export const AutoSaveIndicator = ({ isDraft, status, locale, onRetry }: AutoSaveIndicatorProps) => {
  const { t } = useTranslation();
  const isFailed = isDraft && status.state === "failed";
  const isSaved = isDraft && status.state === "saved";

  const text = useMemo(() => {
    if (!isDraft) return t("workspace.surveys.edit.auto_save_disabled");
    switch (status.state) {
      case "saving":
        return t("workspace.surveys.edit.autosave_saving");
      case "saved":
        return t("workspace.surveys.edit.autosave_saved_at", {
          time: new Intl.DateTimeFormat(locale, { hour: "numeric", minute: "2-digit" }).format(status.at),
        });
      case "failed":
        return t("workspace.surveys.edit.autosave_failed");
      default:
        return t("workspace.surveys.edit.auto_save_on");
    }
  }, [isDraft, status, locale, t]);

  const badge = (
    <span
      className={cn(
        "inline-flex cursor-default items-center rounded-full border px-2.5 py-0.5 text-xs font-medium whitespace-nowrap transition-colors duration-300",
        isFailed && "border-amber-500 bg-amber-50 text-amber-800",
        isSaved && "border-green-600 bg-green-50 text-green-800",
        !isFailed && !isSaved && "border-slate-200 bg-slate-100 text-slate-600"
      )}>
      {/* Announced politely, so a screen reader hears the save land or fail without losing its place. */}
      <span aria-live="polite">{text}</span>
    </span>
  );

  return (
    // The quiet states give way below lg, where the bar needs the room for Save and Publish; a failure always shows.
    <div className={cn("items-center gap-1", isFailed ? "flex" : "hidden lg:flex")}>
      <TooltipRenderer
        shouldRender={!isDraft}
        tooltipContent={t("workspace.surveys.edit.auto_save_disabled_tooltip")}
        className="max-w-64 text-center">
        {badge}
      </TooltipRenderer>
      {isFailed && (
        <Button variant="secondary" size="sm" onClick={onRetry}>
          {t("common.try_again")}
        </Button>
      )}
    </div>
  );
};
