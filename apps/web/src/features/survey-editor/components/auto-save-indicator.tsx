import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "../../../lib/cn";
import { Button } from "../../../components/ui/button";
import { TooltipRenderer } from "../../../components/ui/tooltip";

/** Where the draft's autosave stands: nothing saved yet this session, a save under way, the last success, failures in a row (shown once the editor stops retrying quietly), or a draft the server refused as invalid, with the editor's wording of why. */
export type AutoSaveStatus =
  | { state: "idle" }
  | { state: "saving" }
  | { state: "saved"; at: Date }
  | { state: "failed"; failures: number }
  | { state: "invalid"; reason: string };

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
  const isInvalid = isDraft && status.state === "invalid";
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
      case "invalid":
        return t("workspace.surveys.edit.autosave_not_saved", { reason: status.reason });
      default:
        return t("workspace.surveys.edit.auto_save_on");
    }
  }, [isDraft, status, locale, t]);

  const badge = (
    <span
      className={cn(
        "inline-flex max-w-48 cursor-default items-center rounded-full border px-2.5 py-0.5 text-xs font-medium whitespace-nowrap transition-colors duration-300 lg:max-w-72",
        (isFailed || isInvalid) && "border-amber-500 bg-amber-50 text-amber-800",
        isSaved && "border-green-600 bg-green-50 text-green-800",
        !isFailed && !isInvalid && !isSaved && "border-slate-200 bg-slate-100 text-slate-600"
      )}
      // A long reason is cut short in the bar, more so below lg where the name field needs the room; the whole of it stays on hover and in the live region.
      title={isInvalid ? text : undefined}>
      {/* Announced politely, so a screen reader hears the save land or fail without losing its place. */}
      <span aria-live="polite" className="truncate">
        {text}
      </span>
    </span>
  );

  return (
    // The quiet states give way below lg, where the bar needs the room for Save and Publish; a failure always shows.
    <div className={cn("min-w-0 items-center gap-1", isFailed || isInvalid ? "flex" : "hidden lg:flex")}>
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
