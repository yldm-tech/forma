import { useAutoAnimate } from "@formkit/auto-animate/react";
import type { TSurvey } from "@forma/types/surveys/types";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, ChevronUp, Plus, TrashIcon, XIcon } from "lucide-react";
import { type ButtonHTMLAttributes, Fragment, type ReactNode, forwardRef, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../../components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "../../../../components/ui/popover";
import { Select } from "../../../../components/ui/select";
import { getLocalizedValue } from "../../../../lib/survey/i18n";
import { surveyFilterDataQuery } from "../../api";
import {
  type ElementFilterOptions,
  type ElementOption,
  OptionsType,
  type SelectedFilterValue,
  type TResponseStatus,
} from "../../lib/filter-types";
import { countAppliedFilters } from "../../lib/filter-state";
import {
  NO_VALUE_FILTER_OPERATORS,
  describeFilterCondition,
  generateElementAndFilterOptions,
  getFilterTargetLabel,
} from "../../lib/filters";
import { ElementFilterComboBox } from "./element-filter-combo-box";
import { ElementsComboBox } from "./elements-combo-box";
import { useResponseFilter } from "./response-filter-context";

/** The bordered trigger every control of the filter bar uses. */
export const PopoverTriggerButton = forwardRef<
  HTMLButtonElement,
  ButtonHTMLAttributes<HTMLButtonElement> & { isOpen: boolean; children: ReactNode }
>(({ isOpen, children, ...props }, ref) => (
  <button
    ref={ref}
    type="button"
    {...props}
    className="flex min-w-32 cursor-pointer items-center justify-between rounded-md border border-slate-300 bg-white p-2 hover:border-slate-400 disabled:cursor-not-allowed disabled:opacity-50">
    <span className="text-sm text-slate-700">{children}</span>
    <div className="ml-3">
      {isOpen ? (
        <ChevronUp className="ml-2 size-4 opacity-50" />
      ) : (
        <ChevronDown className="ml-2 size-4 opacity-50" />
      )}
    </div>
  </button>
));
PopoverTriggerButton.displayName = "PopoverTriggerButton";

const getDefaultFilterValue = (option?: ElementFilterOptions): string | undefined => {
  const firstOption = option?.filterOptions[0];
  if (firstOption === undefined) return undefined;
  return typeof firstOption === "object" ? getLocalizedValue(firstOption, "default") : firstOption;
};

const findFilterOption = (options: ElementFilterOptions[], elementType: Partial<ElementOption>) =>
  options.find(
    (option) =>
      (option.type === elementType.elementType || option.type === elementType.type) &&
      option.id === elementType.id
  );

/** forma2's clearItem: rows without a target, or without a value their operator needs, are dropped. */
const withoutIncompleteRows = (value: SelectedFilterValue): SelectedFilterValue => ({
  filter: value.filter.filter(
    (row) =>
      Object.hasOwn(row.elementType, "label") &&
      (Boolean(row.filterType.filterComboBoxValue?.length) ||
        NO_VALUE_FILTER_OPERATORS.includes(row.filterType.filterValue ?? ""))
  ),
  responseStatus: value.responseStatus,
});

/** forma2's ResponseFilter: the popover that edits the filter rows and the response status, applied together. The options are read from the survey's filter data when the popover opens. */
export const ResponseFilter = ({ survey }: { survey: TSurvey }) => {
  const { t } = useTranslation();
  const [parent] = useAutoAnimate();
  const { selectedFilter, setSelectedFilter } = useResponseFilter();
  const [isOpen, setIsOpen] = useState(false);
  const [filterValue, setFilterValue] = useState<SelectedFilterValue>(selectedFilter);
  const filterData = useQuery({ ...surveyFilterDataQuery(survey.id), enabled: isOpen });

  const { elementOptions, elementFilterOptions } = useMemo(() => {
    if (!filterData.data) return { elementOptions: [], elementFilterOptions: [] };
    const { attributes, environmentTags, hiddenFields, reservedValues, variableValues, quotas } =
      filterData.data;
    return generateElementAndFilterOptions({
      survey,
      environmentTags,
      attributes,
      reservedValues,
      hiddenFields,
      variableValues,
      quotas,
      t,
    });
  }, [filterData.data, survey, t]);

  // The applied filter is the editing state's starting point every time it changes from outside (a summary click, a link, clear all).
  const [appliedFilter, setAppliedFilter] = useState(selectedFilter);
  if (appliedFilter !== selectedFilter) {
    setAppliedFilter(selectedFilter);
    setFilterValue(selectedFilter);
  }

  const updateRow = (index: number, row: SelectedFilterValue["filter"][number]) => {
    setFilterValue((current) => ({
      ...current,
      filter: current.filter.map((existing, i) => (i === index ? row : existing)),
    }));
  };

  const handleOnChangeElementComboBoxValue = (value: ElementOption, index: number) => {
    const matchingFilterOption = elementFilterOptions.find(
      (option) => option.type === value.type || option.type === value.elementType
    );
    updateRow(index, {
      elementType: value,
      filterType: {
        filterComboBoxValue: undefined,
        filterValue: getDefaultFilterValue(matchingFilterOption),
      },
    });
  };

  const handleOpenChange = (open: boolean) => {
    setIsOpen(open);
    // A row left without a target or a value is dropped when the popover closes.
    if (!open) setFilterValue((current) => withoutIncompleteRows(current));
  };

  const handleAddNewFilter = () => {
    setFilterValue((current) => ({
      ...current,
      filter: [
        ...current.filter,
        { elementType: {}, filterType: { filterComboBoxValue: undefined, filterValue: undefined } },
      ],
    }));
  };

  const handleClearAllFilters = () => {
    const cleared: SelectedFilterValue = { filter: [], responseStatus: "all" };
    setFilterValue(cleared);
    setSelectedFilter(cleared);
    setIsOpen(false);
  };

  const handleDeleteFilter = (index: number) => {
    setFilterValue((current) => ({ ...current, filter: current.filter.filter((_, i) => i !== index) }));
  };

  const handleApplyFilters = () => {
    setSelectedFilter(withoutIncompleteRows(filterValue));
    setIsOpen(false);
  };

  // An option already used by another row is not offered again.
  const elementComboBoxOptions = elementOptions.map((group) => ({
    ...group,
    option: group.option.filter(
      (option) => !filterValue.filter.some((row) => row.elementType.id === option.id)
    ),
  }));

  // The applied filter, not the rows being edited: a row added but not applied changes nothing yet.
  const activeFilterCount = countAppliedFilters(selectedFilter);

  return (
    <Popover open={isOpen} onOpenChange={handleOpenChange}>
      <PopoverTrigger asChild>
        <PopoverTriggerButton isOpen={isOpen}>
          {t("common.filter")} <b>{activeFilterCount > 0 && `(${activeFilterCount})`}</b>
        </PopoverTriggerButton>
      </PopoverTrigger>
      <PopoverContent
        align="start"
        className="w-[300px] rounded-lg border-slate-200 p-6 sm:w-[400px] md:w-[750px] lg:w-[1000px]"
        onOpenAutoFocus={(event) => event.preventDefault()}>
        <div className="mb-6 flex flex-wrap items-start justify-between gap-2">
          <p className="font-semibold text-slate-800">
            {t("workspace.surveys.summary.show_all_responses_that_match")}
          </p>
          <div className="flex items-center gap-x-2">
            <Select
              aria-label={t("workspace.surveys.summary.show_all_responses_that_match")}
              value={filterValue.responseStatus}
              onChange={(event) =>
                setFilterValue((current) => ({
                  ...current,
                  responseStatus: event.target.value as TResponseStatus,
                }))
              }
              className="text-slate-700">
              <option value="all">{t("workspace.surveys.filter.complete_and_partial_responses")}</option>
              <option value="complete">{t("workspace.surveys.filter.complete_responses")}</option>
              <option value="partial">{t("workspace.surveys.filter.partial_responses")}</option>
            </Select>
          </div>
        </div>

        <div ref={parent}>
          {filterValue.filter.map((row, i) => {
            const filterOption = findFilterOption(elementFilterOptions, row.elementType);
            return (
              // Rows have no identity of their own; their position is what the handlers address.
              // oxlint-disable-next-line react/no-array-index-key -- see above
              <Fragment key={i}>
                <div className="flex w-full flex-wrap gap-3 md:flex-nowrap">
                  <div className="grid w-full grid-cols-1 items-center gap-3 md:grid-cols-2">
                    <ElementsComboBox
                      key={`${row.elementType.label}-${i}-${row.elementType.id}`}
                      options={elementComboBoxOptions}
                      selected={row.elementType}
                      onChangeValue={(value) => handleOnChangeElementComboBoxValue(value, i)}
                    />
                    <ElementFilterComboBox
                      key={`${row.elementType.id}-${i}`}
                      filterOptions={filterOption?.filterOptions}
                      filterComboBoxOptions={filterOption?.filterComboBoxOptions}
                      fieldDataType={filterOption?.fieldDataType}
                      filterValue={row.filterType.filterValue}
                      filterComboBoxValue={row.filterType.filterComboBoxValue}
                      type={
                        row.elementType.type === OptionsType.ELEMENTS
                          ? row.elementType.elementType
                          : row.elementType.type
                      }
                      fieldId={row.elementType.id}
                      handleRemoveMultiSelect={(value) =>
                        updateRow(i, {
                          ...row,
                          filterType: { ...row.filterType, filterComboBoxValue: value },
                        })
                      }
                      onChangeFilterComboBoxValue={(value) =>
                        updateRow(i, {
                          ...row,
                          filterType: { ...row.filterType, filterComboBoxValue: value },
                        })
                      }
                      onChangeFilterValue={(value) =>
                        updateRow(i, {
                          ...row,
                          filterType: { filterComboBoxValue: undefined, filterValue: value },
                        })
                      }
                      disabled={!row.elementType.label}
                    />
                  </div>
                  <div className="flex w-full items-center justify-end gap-1 md:w-auto">
                    <Button
                      variant="secondary"
                      size="icon"
                      onClick={() => handleDeleteFilter(i)}
                      aria-label={t("common.delete")}>
                      <TrashIcon />
                    </Button>
                  </div>
                </div>
                {i !== filterValue.filter.length - 1 && (
                  <div className="my-4 flex items-center">
                    <p className="mr-4 font-semibold text-slate-800">{t("common.and")}</p>
                    <hr className="w-full text-slate-600" />
                  </div>
                )}
              </Fragment>
            );
          })}
        </div>
        <div className="mt-6 flex items-center justify-between">
          <div className="flex gap-2">
            <Button size="sm" variant="secondary" onClick={handleAddNewFilter}>
              {t("common.add_filter")}
              <Plus />
            </Button>
            <Button size="sm" onClick={handleApplyFilters}>
              {t("common.apply_filters")}
            </Button>
          </div>
          <Button size="sm" variant="ghost" onClick={handleClearAllFilters}>
            {t("common.clear_all")}
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  );
};

/** The applied conditions as chips beside the filter bar, each removable on its own, so a filtered view says what it is filtered by. */
export const AppliedFilterChips = () => {
  const { t } = useTranslation();
  const { selectedFilter, setSelectedFilter } = useResponseFilter();
  const statusLabel: Record<TResponseStatus, string | null> = {
    all: null,
    complete: t("workspace.surveys.filter.complete_responses"),
    partial: t("workspace.surveys.filter.partial_responses"),
  };
  const chips = [
    ...selectedFilter.filter.map((row, index) => ({
      key: `${row.elementType.id ?? ""}-${index}`,
      label: getFilterTargetLabel(row.elementType, t),
      condition: describeFilterCondition(row, t),
      remove: () =>
        setSelectedFilter((current) => ({
          ...current,
          filter: current.filter.filter((_, i) => i !== index),
        })),
    })),
    ...(statusLabel[selectedFilter.responseStatus]
      ? [
          {
            key: "responseStatus",
            label: statusLabel[selectedFilter.responseStatus] ?? "",
            condition: "",
            remove: () => setSelectedFilter((current) => ({ ...current, responseStatus: "all" })),
          },
        ]
      : []),
  ];
  if (chips.length === 0) return null;
  return (
    <ul
      className="flex flex-wrap items-center gap-1.5"
      aria-label={t("workspace.surveys.summary.applied_filters")}>
      {chips.map((chip) => (
        <li
          key={chip.key}
          className="flex h-9 max-w-96 items-center gap-1.5 rounded-md border border-slate-200 bg-slate-50 pr-1 pl-2.5 text-sm text-slate-700">
          {/* A long question is cut, never the condition after it. */}
          <span className="min-w-0 truncate" title={chip.label}>
            {chip.label}
          </span>
          {chip.condition && <span className="shrink-0 font-medium text-slate-900">{chip.condition}</span>}
          <button
            type="button"
            className="focus-visible:ring-ring shrink-0 cursor-pointer rounded-sm p-1 text-slate-500 hover:bg-slate-200 hover:text-slate-800 focus-visible:ring-2 focus-visible:outline-hidden"
            aria-label={t("workspace.surveys.summary.remove_filter", {
              filter: [chip.label, chip.condition].filter(Boolean).join(" "),
            })}
            onClick={chip.remove}>
            <XIcon className="size-3.5" />
          </button>
        </li>
      ))}
    </ul>
  );
};
