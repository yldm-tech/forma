import type { TSurvey } from "@forma/types/surveys/types";
import type { TFunction } from "i18next";
import { Loader2 } from "lucide-react";
import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { DateRangeCalendar } from "../../../../components/ui/date-picker";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "../../../../components/ui/dropdown-menu";
import { formatDateForDisplay } from "../../../../lib/survey/datetime";
import { toast } from "../../../../lib/toast";
import { useClickOutside } from "../../../../lib/use-click-outside";
import { type ExportFormat, analysisApi } from "../../api";
import {
  type TDateRangePreset,
  resolveDateRangeLabelPreset,
  resolveDateRangePresetBounds,
} from "../../lib/date-ranges";
import { saveExport } from "../../lib/download";
import type { DateRange } from "../../lib/filter-types";
import { getTodayDate } from "../../lib/filters";
import { PopoverTriggerButton, ResponseFilter } from "./response-filter";
import { useResponseFilter } from "./response-filter-context";

// The relative ranges this filter offers, in dropdown order. Picking one tags the range with its preset, so the trigger label survives a remount without reverse-matching the bounds: several presets span identical days on period-boundary dates and cannot be told apart from their bounds alone. Order still breaks that tie for a hand-picked range that matches a preset. Labels are literal t() calls so the translation-key scan sees them.
const DATE_RANGE_PRESETS: readonly { preset: TDateRangePreset; getLabel: (t: TFunction) => string }[] = [
  { preset: "last 7 days", getLabel: (t) => t("workspace.surveys.summary.last_7_days") },
  { preset: "last 30 days", getLabel: (t) => t("workspace.surveys.summary.last_30_days") },
  { preset: "this month", getLabel: (t) => t("workspace.surveys.summary.this_month") },
  { preset: "last month", getLabel: (t) => t("workspace.surveys.summary.last_month") },
  { preset: "this quarter", getLabel: (t) => t("workspace.surveys.summary.this_quarter") },
  { preset: "last quarter", getLabel: (t) => t("workspace.surveys.summary.last_quarter") },
  { preset: "last 6 months", getLabel: (t) => t("workspace.surveys.summary.last_6_months") },
  { preset: "this year", getLabel: (t) => t("workspace.surveys.summary.this_year") },
  { preset: "last year", getLabel: (t) => t("workspace.surveys.summary.last_year") },
];

const DATE_RANGE_PRESET_NAMES = DATE_RANGE_PRESETS.map(({ preset }) => preset);

const DAY_MONTH_OPTIONS: Intl.DateTimeFormatOptions = { day: "numeric", month: "short" };

type RangeMode = "allTime" | "preset" | "custom";

const getCustomRangeLabel = (dateRange: DateRange, locale: string, t: TFunction): string => {
  const from = dateRange.from
    ? formatDateForDisplay(dateRange.from, locale, DAY_MONTH_OPTIONS)
    : t("workspace.surveys.summary.select_first_date");
  const to = dateRange.to
    ? formatDateForDisplay(dateRange.to, locale, DAY_MONTH_OPTIONS)
    : t("workspace.surveys.summary.select_last_date");
  return `${from} - ${to}`;
};

/** The label the date trigger shows: all time, the preset's name, or the custom range's bounds. */
const getRangeLabel = (mode: RangeMode, dateRange: DateRange, locale: string, t: TFunction): string => {
  if (mode === "allTime" || (mode === "preset" && !dateRange.from))
    return t("workspace.surveys.summary.all_time");
  if (mode === "preset") {
    const preset = resolveDateRangeLabelPreset(dateRange, DATE_RANGE_PRESET_NAMES);
    const matched = DATE_RANGE_PRESETS.find((option) => option.preset === preset);
    if (matched) return matched.getLabel(t);
  }
  return getCustomRangeLabel(dateRange, locale, t);
};

/** forma2's CustomFilter: the filter popover, the date range and the response download, above the summary and the response table. */
export const CustomFilter = ({ survey }: { survey: TSurvey }) => {
  const { t, i18n } = useTranslation();
  const locale = i18n.resolvedLanguage ?? i18n.language ?? "en-US";
  const { dateRange, setDateRange, filterCriteria } = useResponseFilter();
  const [rangeMode, setRangeMode] = useState<RangeMode>(() => {
    if (!dateRange.from || !dateRange.to) return "allTime";
    return resolveDateRangeLabelPreset(dateRange, DATE_RANGE_PRESET_NAMES) ? "preset" : "custom";
  });
  const [isDatePickerOpen, setIsDatePickerOpen] = useState(false);
  const [isRangeDropDownOpen, setIsRangeDropDownOpen] = useState(false);
  const [isDownloadDropDownOpen, setIsDownloadDropDownOpen] = useState(false);
  const [isDownloading, setIsDownloading] = useState(false);
  const datePickerRef = useRef<HTMLDivElement>(null);

  useClickOutside(datePickerRef, () => setIsDatePickerOpen(false));

  // "All" sends no criteria at all, "Filtered" the current ones, as forma2 did.
  const handleDownloadResponses = async (filtered: boolean, format: ExportFormat) => {
    setIsDownloading(true);
    try {
      const file = await analysisApi.exportResponses(survey.id, format, filtered ? filterCriteria : {});
      saveExport(file, format);
    } catch {
      toast.error(t("workspace.surveys.responses.error_downloading_responses"));
    } finally {
      setIsDownloading(false);
    }
  };

  const downloadItems: { testId: string; label: string; filtered: boolean; format: ExportFormat }[] = [
    {
      testId: "fb__custom-filter-download-all-csv",
      label: t("workspace.surveys.summary.all_responses_csv"),
      filtered: false,
      format: "csv",
    },
    {
      testId: "fb__custom-filter-download-all-xlsx",
      label: t("workspace.surveys.summary.all_responses_excel"),
      filtered: false,
      format: "xlsx",
    },
    {
      testId: "fb__custom-filter-download-filtered-csv",
      label: t("workspace.surveys.summary.filtered_responses_csv"),
      filtered: true,
      format: "csv",
    },
    {
      testId: "fb__custom-filter-download-filtered-xlsx",
      label: t("workspace.surveys.summary.filtered_responses_excel"),
      filtered: true,
      format: "xlsx",
    },
  ];

  return (
    <div className="relative flex justify-between">
      <div className="flex justify-stretch gap-x-1.5">
        <ResponseFilter survey={survey} />
        <DropdownMenu
          onOpenChange={(open) => {
            if (open) setIsDatePickerOpen(false);
            setIsRangeDropDownOpen(open);
          }}>
          <DropdownMenuTrigger asChild>
            <PopoverTriggerButton isOpen={isRangeDropDownOpen}>
              {getRangeLabel(rangeMode, dateRange, locale, t)}
            </PopoverTriggerButton>
          </DropdownMenuTrigger>
          <DropdownMenuContent>
            <DropdownMenuItem
              onClick={() => {
                setRangeMode("allTime");
                setDateRange({ from: undefined, to: getTodayDate() });
              }}>
              <p className="text-slate-700">{t("workspace.surveys.summary.all_time")}</p>
            </DropdownMenuItem>
            {DATE_RANGE_PRESETS.map(({ preset, getLabel }) => (
              <DropdownMenuItem
                key={preset}
                onClick={() => {
                  setRangeMode("preset");
                  setDateRange({ ...resolveDateRangePresetBounds(preset), preset });
                }}>
                <p className="text-slate-700">{getLabel(t)}</p>
              </DropdownMenuItem>
            ))}
            <DropdownMenuItem
              onClick={() => {
                setIsDatePickerOpen(true);
                setRangeMode("custom");
              }}>
              <p className="text-sm text-slate-700 hover:ring-0">
                {t("workspace.surveys.summary.custom_range")}
              </p>
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        <DropdownMenu
          onOpenChange={(open) => {
            if (open) setIsDatePickerOpen(false);
            setIsDownloadDropDownOpen(open);
          }}>
          <DropdownMenuTrigger asChild>
            <PopoverTriggerButton isOpen={isDownloadDropDownOpen} disabled={isDownloading}>
              <span className="flex items-center gap-2">
                {t("common.download")}
                {isDownloading && <Loader2 className="size-3 animate-spin" strokeWidth={1.5} />}
              </span>
            </PopoverTriggerButton>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start">
            {downloadItems.map((item) => (
              <DropdownMenuItem
                key={item.testId}
                data-testid={item.testId}
                onClick={() => void handleDownloadResponses(item.filtered, item.format)}>
                <p className="text-slate-700">{item.label}</p>
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      {isDatePickerOpen && (
        <div ref={datePickerRef} className="absolute top-full z-50 my-2 rounded-md border bg-white">
          <DateRangeCalendar
            value={dateRange}
            locale={locale}
            // A hand-picked range carries no preset: the label shows its bounds.
            onChange={(range) => setDateRange({ from: range.from, to: range.to })}
            onComplete={() => setIsDatePickerOpen(false)}
          />
        </div>
      )}
    </div>
  );
};
