import type { TEmbeddedDataType } from "@forma/types/embedded-data";
import type { TI18nString } from "@forma/types/i18n";
import type { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type { TDateRangePreset } from "./date-ranges";

// The analysis filter's state, forma2's response-filter-context, ElementsComboBox and ResponseFilter types: what the filter bar holds and what getFormattedFilters turns into the server's criteria.

/** The groups the filter picker lists its options under. The values are part of the shareable ?filter= param. */
export enum OptionsType {
  ELEMENTS = "Elements",
  TAGS = "Tags",
  ATTRIBUTES = "Attributes",
  OTHERS = "Other Filters",
  META = "Meta",
  HIDDEN_FIELDS = "Hidden Fields",
  VARIABLES = "Variables",
  QUOTAS = "Quotas",
  SEGMENTS = "Segments",
}

/** A segment the filter offers, as the survey's filter data lists it. */
export interface TFilterSegment {
  id: string;
  title: string;
}

/** A contact attribute key's data type (contact_attribute_keys.data_type). */
export type TContactAttributeDataType = "string" | "number" | "date";

/** One thing a filter row can filter on: an element, a tag, an attribute, a field, a quota or a segment. */
export interface ElementOption {
  label: string;
  elementType?: TSurveyElementTypeEnum;
  type: OptionsType;
  id: string;
  /** A contact attribute's type, which decides its operators and how its value is sent. Carried in the row (and in ?filter=), since the criteria are built without the filter data. */
  dataType?: TContactAttributeDataType;
}

export interface ElementOptions {
  header: OptionsType;
  option: ElementOption[];
}

/** The operators and values a filter row offers for one option. */
export interface ElementFilterOptions {
  type:
    | TSurveyElementTypeEnum
    | "Attributes"
    | "Tags"
    | "Languages"
    | "Quotas"
    | "Segments"
    | "Hidden Fields"
    | "Meta"
    | "Variables"
    | OptionsType.OTHERS;
  filterOptions: (string | TI18nString)[];
  filterComboBoxOptions: (string | TI18nString)[];
  /** For Embedded Data and reserved fields: drives the operator set and the value input type. */
  fieldDataType?: TEmbeddedDataType;
  id: string;
}

/** One filter row: what it filters on, its operator and its value. */
export interface FilterValue {
  elementType: Partial<ElementOption>;
  filterType: {
    filterValue: string | undefined;
    filterComboBoxValue: string | string[] | undefined;
  };
}

export type TResponseStatus = "all" | "complete" | "partial";

export interface SelectedFilterValue {
  filter: FilterValue[];
  responseStatus: TResponseStatus;
  /** The keyword search over the response table: a word one of the response's free-text answers holds. Absent or empty when there is none. */
  keyword?: string;
}

export interface DateRange {
  from: Date | undefined;
  to?: Date;
  /** Which preset produced this range, if any: set by the preset dropdown, cleared by the manual calendar picker. The label is read from here instead of reverse-matching the range, since several presets span identical days on period-boundary dates (e.g. "this month" and "last 7 days" on the 7th of any month) and cannot be told apart by their bounds alone. */
  preset?: TDateRangePreset;
}
