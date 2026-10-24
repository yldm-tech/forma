import type { TSegmentConnector } from "@forma/types/segment";
import { useTranslation } from "react-i18next";
import { cn } from "../../../lib/cn";

/**
 * The connector column of a filter or group: plain "Where" text before the first, then a pill with "and" or "or" that switches to the other on click. Both segment editors use it; its fixed width keeps every row's fields in one column, also for CJK labels.
 */
export const SegmentConnector = ({
  connector,
  viewOnly,
  onToggle,
  className,
}: {
  connector: TSegmentConnector;
  viewOnly?: boolean;
  onToggle: (next: "and" | "or") => void;
  className?: string;
}) => {
  const { t } = useTranslation();
  const label = (value: "and" | "or") =>
    value === "and" ? t("workspace.segments.connector_and") : t("workspace.segments.connector_or");
  if (!connector) {
    return (
      <div className={cn("flex h-9 w-16 shrink-0 items-center", className)}>
        <span className="text-sm whitespace-nowrap text-slate-600">{t("workspace.segments.where")}</span>
      </div>
    );
  }
  const next = connector === "and" ? "or" : "and";
  const toggleLabel = t("workspace.segments.toggle_connector", {
    current: label(connector),
    next: label(next),
  });
  return (
    <div className={cn("flex h-9 w-16 shrink-0 items-center", className)}>
      <button
        type="button"
        disabled={viewOnly}
        aria-label={toggleLabel}
        title={toggleLabel}
        className="inline-flex h-7 min-w-10 items-center justify-center rounded-full border border-slate-300 bg-white px-2.5 text-xs font-medium whitespace-nowrap text-slate-700 hover:border-slate-400 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-60"
        onClick={() => onToggle(next)}>
        {label(connector)}
      </button>
    </div>
  );
};
