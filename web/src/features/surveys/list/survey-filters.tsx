import { ChevronDownIcon, SearchIcon, XIcon } from "lucide-react";
import { type Dispatch, Fragment, type SetStateAction, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../components/ui/button";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "../../../components/ui/dropdown-menu";
import { cn } from "../../../lib/cn";
import {
  type SurveyFilters as Filters,
  type SurveySort,
  type SurveyStatusFilter,
  type SurveyTypeFilter,
  type WorkspaceChannel,
  hasActiveFilters,
  initialFilters,
} from "./filters";

/** How long typing in the search box waits before the list is asked again (forma2's 800 ms). */
const NAME_DEBOUNCE_MS = 800;

interface FilterOption<T extends string> {
  label: string;
  value: T;
  /** A divider above the option, setting it apart from the ones before it. */
  separatorBefore?: boolean;
}

const FilterDropdown = <T extends string>({
  title,
  options,
  selected,
  onToggle,
}: {
  title: string;
  options: FilterOption<T>[];
  selected: readonly T[];
  onToggle: (value: T) => void;
}) => (
  <DropdownMenu>
    <DropdownMenuTrigger asChild>
      <button
        type="button"
        className={cn(
          "flex h-8 items-center rounded-md border border-slate-700 px-2 text-sm outline-hidden focus-visible:ring-2 focus-visible:ring-slate-400",
          selected.length > 0 ? "bg-slate-900 text-white" : "hover:bg-slate-900 hover:text-white"
        )}>
        {title}
        <ChevronDownIcon className="ml-2 size-4" />
      </button>
    </DropdownMenuTrigger>
    <DropdownMenuContent align="start">
      {options.map((option) => (
        <Fragment key={option.value}>
          {option.separatorBefore && <DropdownMenuSeparator />}
          <DropdownMenuCheckboxItem
            checked={selected.includes(option.value)}
            onSelect={(event) => {
              event.preventDefault();
              onToggle(option.value);
            }}>
            {option.label}
          </DropdownMenuCheckboxItem>
        </Fragment>
      ))}
    </DropdownMenuContent>
  </DropdownMenu>
);

const toggled = <T,>(list: readonly T[], value: T): T[] =>
  list.includes(value) ? list.filter((v) => v !== value) : [...list, value];

/** forma2's SurveyFilters: a debounced name search, status and type filters, "Clear filters", and the sort order. */
export const SurveyFilters = ({
  filters,
  setFilters,
  channel,
}: {
  filters: Filters;
  setFilters: Dispatch<SetStateAction<Filters>>;
  channel: WorkspaceChannel;
}) => {
  const { t } = useTranslation();
  const [name, setName] = useState(filters.name);

  useEffect(() => {
    const timeout = setTimeout(() => setFilters((prev) => ({ ...prev, name })), NAME_DEBOUNCE_MS);
    return () => clearTimeout(timeout);
  }, [name, setFilters]);

  const statusOptions: FilterOption<SurveyStatusFilter>[] = [
    { label: t("common.draft"), value: "draft" },
    { label: t("common.in_progress"), value: "inProgress" },
    { label: t("common.paused"), value: "paused" },
    { label: t("common.completed"), value: "completed" },
    { label: t("common.archived"), value: "archived", separatorBefore: true },
  ];
  const typeOptions: FilterOption<SurveyTypeFilter>[] = [
    { label: t("common.link"), value: "link" },
    { label: t("common.app"), value: "app" },
  ];
  const sortOptions: { label: string; value: SurveySort }[] = [
    { label: t("common.updated_at"), value: "updatedAt" },
    { label: t("common.created_at"), value: "createdAt" },
    { label: t("workspace.surveys.alphabetical"), value: "name" },
    { label: t("workspace.surveys.relevance"), value: "relevance" },
  ];
  // The typed name counts before it reaches the list, as forma2's toolbar showed the button.
  const showClear = hasActiveFilters({ ...filters, name });

  return (
    <div className="flex flex-wrap justify-between gap-2">
      <div className="flex flex-wrap gap-2">
        <label className="flex h-8 items-center gap-2 rounded-lg border border-slate-700 bg-white px-4 text-slate-800">
          <SearchIcon className="size-4" aria-hidden="true" />
          <span className="sr-only">{t("workspace.surveys.search_by_survey_name")}</span>
          <input
            type="search"
            className="w-full border-none bg-transparent text-sm focus:outline-hidden"
            placeholder={t("workspace.surveys.search_by_survey_name")}
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </label>
        <FilterDropdown
          title={t("common.status")}
          options={statusOptions}
          selected={filters.status}
          onToggle={(value) => setFilters((prev) => ({ ...prev, status: toggled(prev.status, value) }))}
        />
        {channel !== "link" && (
          <FilterDropdown
            title={t("common.type")}
            options={typeOptions}
            selected={filters.type}
            onToggle={(value) => setFilters((prev) => ({ ...prev, type: toggled(prev.type, value) }))}
          />
        )}
        {showClear && (
          <Button
            size="sm"
            onClick={() => {
              setFilters(initialFilters);
              setName(initialFilters.name);
            }}>
            {t("common.clear_filters")}
            <XIcon />
          </Button>
        )}
      </div>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <button
            type="button"
            className="flex h-8 items-center rounded-md border border-slate-700 px-2 text-sm outline-hidden hover:bg-slate-900 hover:text-white focus-visible:ring-2 focus-visible:ring-slate-400">
            {t("common.sort_by")}: {sortOptions.find((option) => option.value === filters.sortBy)?.label}
            <ChevronDownIcon className="ml-2 size-4" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuRadioGroup
            value={filters.sortBy}
            onValueChange={(value) => setFilters((prev) => ({ ...prev, sortBy: value as SurveySort }))}>
            {sortOptions.map((option) => (
              <DropdownMenuRadioItem key={option.value} value={option.value}>
                {option.label}
              </DropdownMenuRadioItem>
            ))}
          </DropdownMenuRadioGroup>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
};
