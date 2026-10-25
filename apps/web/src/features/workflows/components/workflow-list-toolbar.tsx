import { ChevronDownIcon, SearchIcon, XIcon } from "lucide-react";
import { Fragment } from "react";
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
import type { TWorkflowSortBy, TWorkflowStatus } from "../definition";
import type { TStoredWorkflowFilters } from "../lib/list-filters";

interface StatusOption {
  label: string;
  value: TWorkflowStatus;
  separatorBefore?: boolean;
}

/** forma2's workflows list toolbar: the name search, the status filter, "Clear filters" and the sort order. */
export const WorkflowListToolbar = ({
  filters,
  onChange,
}: Readonly<{
  filters: TStoredWorkflowFilters;
  onChange: (filters: TStoredWorkflowFilters) => void;
}>) => {
  const { t } = useTranslation();

  // Archived is one more status, set apart because it is the only one the default list leaves out.
  const statusOptions: StatusOption[] = [
    { label: t("common.draft"), value: "draft" },
    { label: t("common.enabled"), value: "enabled" },
    { label: t("common.disabled"), value: "disabled" },
    { label: t("common.archived"), value: "archived", separatorBefore: true },
  ];
  const sortOptions: { label: string; value: TWorkflowSortBy }[] = [
    { label: t("common.updated_at"), value: "updatedAt" },
    { label: t("common.created_at"), value: "createdAt" },
    { label: t("workspace.workflows.alphabetical"), value: "name" },
  ];
  const activeSortLabel = sortOptions.find((option) => option.value === filters.sortBy)?.label ?? "";
  const hasActiveFilters = filters.selectedStatuses.length > 0 || filters.searchValue.length > 0;

  const toggleStatus = (value: TWorkflowStatus) =>
    onChange({
      ...filters,
      selectedStatuses: filters.selectedStatuses.includes(value)
        ? filters.selectedStatuses.filter((status) => status !== value)
        : [...filters.selectedStatuses, value],
    });

  return (
    <div className="flex flex-wrap items-center justify-between gap-4">
      <div className="flex items-center gap-x-2">
        <label className="flex h-8 w-80 items-center gap-2 rounded-lg border border-slate-700 bg-white px-4 text-slate-800">
          <SearchIcon className="size-4" aria-hidden="true" />
          <span className="sr-only">{t("workspace.workflows.search_by_workflow_name")}</span>
          <input
            type="search"
            className="w-full border-none bg-transparent text-sm focus:outline-hidden"
            placeholder={t("workspace.workflows.search_by_workflow_name")}
            value={filters.searchValue}
            onChange={(event) => onChange({ ...filters, searchValue: event.target.value })}
          />
        </label>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button
              type="button"
              className={cn(
                "flex h-8 items-center rounded-md border border-slate-700 px-2 text-sm outline-hidden focus-visible:ring-2 focus-visible:ring-ring",
                filters.selectedStatuses.length > 0
                  ? "bg-slate-900 text-white"
                  : "hover:bg-slate-900 hover:text-white"
              )}>
              {t("common.status")}
              <ChevronDownIcon className="ml-2 size-4" aria-hidden="true" />
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start">
            {statusOptions.map((option) => (
              <Fragment key={option.value}>
                {option.separatorBefore && <DropdownMenuSeparator />}
                <DropdownMenuCheckboxItem
                  checked={filters.selectedStatuses.includes(option.value)}
                  onSelect={(event) => {
                    event.preventDefault();
                    toggleStatus(option.value);
                  }}>
                  {option.label}
                </DropdownMenuCheckboxItem>
              </Fragment>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
        {hasActiveFilters ? (
          <Button
            size="sm"
            className="h-8"
            onClick={() => onChange({ ...filters, searchValue: "", selectedStatuses: [] })}>
            {t("common.clear_filters")}
            <XIcon />
          </Button>
        ) : null}
      </div>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <button
            type="button"
            aria-label={t("common.sort_by")}
            className="focus-visible:ring-ring flex h-8 items-center rounded-md border border-slate-700 px-2 text-sm outline-hidden hover:bg-slate-900 hover:text-white focus-visible:ring-2">
            <span className="whitespace-nowrap">{t("common.sort_by_value", { label: activeSortLabel })}</span>
            <ChevronDownIcon className="ml-2 size-4" aria-hidden="true" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuRadioGroup
            value={filters.sortBy}
            onValueChange={(value) => onChange({ ...filters, sortBy: value as TWorkflowSortBy })}>
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
