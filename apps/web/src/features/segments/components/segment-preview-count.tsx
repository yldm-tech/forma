import { ZSegmentFilters, type TBaseFilters } from "@forma/types/segment";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { AlertTriangleIcon, UsersIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { cn } from "../../../lib/cn";
import { useDebouncedValue } from "../../../lib/use-debounced-value";
import { segmentPreviewCountQuery } from "../api";

/** How long the tree must stay unchanged before it is counted, so typing a value sends one request, not one per key. */
export const PREVIEW_COUNT_DEBOUNCE_MS = 400;

/**
 * How many of the workspace's contacts the filter tree matches, shown under the tree while it is edited. The tree is counted once it has stayed unchanged for a moment and the schema accepts it; while a filter is incomplete the last count stays. Survey-interaction filters are counted exactly; device filters depend on the device a survey is shown on, so they are left out of the count, which then says it is approximate; a count of zero is a warning, since the segment would reach nobody.
 */
export const SegmentPreviewCount = ({
  workspaceId,
  filters,
}: {
  workspaceId: string;
  filters: TBaseFilters;
}) => {
  const { t } = useTranslation();
  const settled = useDebouncedValue(filters, PREVIEW_COUNT_DEBOUNCE_MS);
  const valid = ZSegmentFilters.safeParse(settled).success;
  const preview = useQuery({
    ...segmentPreviewCountQuery(workspaceId, settled),
    enabled: valid,
    placeholderData: keepPreviousData,
  });

  if (preview.isError) {
    return (
      <p role="status" className="text-sm text-slate-500">
        {t("workspace.segments.preview_count_failed")}
      </p>
    );
  }
  if (!preview.data) return null;
  const { count, total, approximate } = preview.data;
  const none = count === 0;
  const Icon = none ? AlertTriangleIcon : UsersIcon;
  return (
    <div
      role="status"
      data-testid="segment-preview-count"
      className={cn(
        "flex flex-col gap-0.5 rounded-md border px-3 py-2 text-sm",
        none ? "border-amber-300 bg-amber-50 text-amber-900" : "border-slate-200 bg-white text-slate-700"
      )}>
      <p className="flex items-center gap-2 font-medium">
        <Icon className="size-4 shrink-0" aria-hidden />
        {t("workspace.segments.preview_count", { count, total })}
      </p>
      {approximate && (
        <p className={cn("text-xs", none ? "text-amber-800" : "text-slate-500")}>
          {t("workspace.segments.preview_count_approximate")}
        </p>
      )}
    </div>
  );
};
