import type { TFunction } from "i18next";
import { TEmbeddedDataType } from "@forma/types/embedded-data";
import { getComputedEmbeddedFields, getIngestedEmbeddedFields } from "@forma/types/embedded-data-resolver";
import { TSurveyQuota } from "@forma/types/quota";
import {
  TResponseFilterCriteria,
  TResponseHiddenFieldsFilter,
  TSurveyContactAttributes,
  TSurveyMetaFieldFilter,
} from "@forma/types/responses";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import { TSurvey } from "@forma/types/surveys/types";
import { getTextContent } from "@forma/types/surveys/validation";
import { TTag } from "@forma/types/tags";
import { getLocalizedValue } from "../../../lib/survey/i18n";
import { recallToHeadline } from "../../../lib/survey/recall";
import { getElementsFromBlocks } from "../../survey-editor/lib/client-utils";
import {
  type DateRange,
  type ElementFilterOptions,
  type ElementOption,
  type ElementOptions,
  type FilterValue,
  OptionsType,
  type SelectedFilterValue,
  type TContactAttributeDataType,
} from "./filter-types";
import { getReservedFieldLabel, getReservedFilterEntries } from "./reserved-fields";

const conditionOptions = {
  openText: ["is", "Contains", "Does not contain"],
  multipleChoiceSingle: ["Includes either"],
  multipleChoiceMulti: ["Includes all", "Includes either"],
  nps: ["Is equal to", "Is less than", "Is more than", "Submitted", "Skipped", "Includes either"],
  rating: ["Is equal to", "Is less than", "Is more than", "Submitted", "Skipped"],
  csat: ["Is equal to", "Is less than", "Is more than", "Submitted", "Skipped"],
  ces: ["Is equal to", "Is less than", "Is more than", "Submitted", "Skipped"],
  // A slider's value is typed (ElementFilterComboBox), not picked: its scale can have a thousand positions.
  slider: ["Is equal to", "Is less than", "Is more than", "Submitted", "Skipped"],
  cta: ["is"],
  tags: ["is"],
  languages: ["Equals", "Not equals"],
  pictureSelection: ["Includes all", "Includes either"],
  consent: ["is"],
  matrix: [""],
  address: ["is"],
  contactInfo: ["is"],
  ranking: ["is"],
} satisfies Record<string, string[]>;
// Rating and CES values come from the element's own range (scaleValues), not from this table.
const filterOptions = {
  openText: ["Filled out", "Skipped"],
  nps: ["0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10"],
  csat: ["1", "2", "3", "4", "5"],
  cta: ["Clicked", "Dismissed"],
  tags: ["Applied", "Not applied"],
  consent: ["Accepted", "Dismissed"],
  address: ["Filled out", "Skipped"],
  contactInfo: ["Filled out", "Skipped"],
  ranking: ["Filled out", "Skipped"],
} satisfies Record<string, string[]>;

/**
 * The operators a summary card puts in the filter when it is clicked. The English strings in the tables above and below are wire values, not labels: getFormattedFilters matches them and ?filter= carries them, so a click must pass these, never a translated label.
 */
export const SUMMARY_FILTER_OPERATOR = {
  includesEither: "Includes either",
  includesAll: "Includes all",
  isEqualTo: "Is equal to",
  isLessThan: "Is less than",
  skipped: "Skipped",
  is: "is",
} as const;

/** The constant values a summary card can filter by: consent's two outcomes. */
export const SUMMARY_FILTER_VALUE = {
  accepted: "Accepted",
  dismissed: "Dismissed",
} as const;

/** A picture selection filter value: the choice's 1-based position, which processPictureSelectionFilter reads back. */
export const pictureFilterValue = (position: number): string => `Picture ${position}`;

const PICTURE_FILTER_VALUE = /^Picture (\d+)$/;

/** What a filter row's operator and value slots belong to: an element type, or an OptionsType group for everything else. */
type TFilterRowType = string | undefined;

type TFilterLabel = (t: TFunction) => string;

// Wire value → label. Each entry calls t() with a literal key so the catalogue key check sees it.
const FILTER_OPERATOR_LABELS: Record<string, TFilterLabel> = {
  "Includes either": (t) => t("workspace.surveys.summary.includes_either"),
  "Includes all": (t) => t("workspace.surveys.summary.includes_all"),
  "Is equal to": (t) => t("workspace.surveys.summary.is_equal_to"),
  "Is less than": (t) => t("workspace.surveys.summary.is_less_than"),
  "Is more than": (t) => t("workspace.analysis.filter_op.is_more_than"),
  Submitted: (t) => t("workspace.analysis.filter_op.submitted"),
  Skipped: (t) => t("common.skipped"),
  is: (t) => t("workspace.analysis.filter_op.is"),
  Equals: (t) => t("workspace.analysis.filter_op.equals"),
  "Not equals": (t) => t("workspace.analysis.filter_op.not_equals"),
  Contains: (t) => t("workspace.analysis.filter_op.contains"),
  "Does not contain": (t) => t("workspace.analysis.filter_op.does_not_contain"),
  "Starts with": (t) => t("workspace.analysis.filter_op.starts_with"),
  "Does not start with": (t) => t("workspace.analysis.filter_op.does_not_start_with"),
  "Ends with": (t) => t("workspace.analysis.filter_op.ends_with"),
  "Does not end with": (t) => t("workspace.analysis.filter_op.does_not_end_with"),
  "Is greater than": (t) => t("workspace.analysis.filter_op.is_greater_than"),
  "Is before": (t) => t("workspace.analysis.filter_op.is_before"),
  "Is after": (t) => t("workspace.analysis.filter_op.is_after"),
  "Is on": (t) => t("workspace.analysis.filter_op.is_on"),
  "Is set": (t) => t("workspace.analysis.filter_op.is_set"),
  "Is not set": (t) => t("workspace.analysis.filter_op.is_not_set"),
  Status: (t) => t("common.status"),
};

const FILTER_VALUE_LABELS: Record<string, TFilterLabel> = {
  "Filled out": (t) => t("workspace.analysis.filter_op.filled_out"),
  Skipped: (t) => t("common.skipped"),
  Clicked: (t) => t("workspace.analysis.filter_op.clicked"),
  Dismissed: (t) => t("common.dismissed"),
  Accepted: (t) => t("common.accepted"),
  Applied: (t) => t("workspace.analysis.filter_op.applied"),
  "Not applied": (t) => t("workspace.analysis.filter_op.not_applied"),
  "Screened in": (t) => t("workspace.analysis.filter_op.screened_in"),
  "Screened out (overquota)": (t) => t("workspace.analysis.filter_op.screened_out_overquota"),
  "Not in quota": (t) => t("workspace.analysis.filter_op.not_in_quota"),
};

// Rows whose values are the fixed vocabulary above. Every other row's values are the respondents' or the creator's own text (choice labels, observed field values, language codes), shown as they are even when one happens to read "Skipped".
const CONSTANT_VALUE_ROW_TYPES = new Set<string>([
  TSurveyElementTypeEnum.OpenText,
  TSurveyElementTypeEnum.Address,
  TSurveyElementTypeEnum.ContactInfo,
  TSurveyElementTypeEnum.Ranking,
  TSurveyElementTypeEnum.CTA,
  TSurveyElementTypeEnum.Consent,
  OptionsType.TAGS,
  OptionsType.QUOTAS,
]);

/** The label of a filter row's operator. A matrix row's operator slot holds the matrix row's own label, shown as it is. */
export const getFilterOperatorLabel = (type: TFilterRowType, operator: string, t: TFunction): string => {
  if (type === TSurveyElementTypeEnum.Matrix) return operator;
  const label = Object.hasOwn(FILTER_OPERATOR_LABELS, operator)
    ? FILTER_OPERATOR_LABELS[operator]
    : undefined;
  return label ? label(t) : operator;
};

/** The label of one of a filter row's values. */
export const getFilterValueLabel = (type: TFilterRowType, value: string, t: TFunction): string => {
  if (type === TSurveyElementTypeEnum.PictureSelection) {
    const position = PICTURE_FILTER_VALUE.exec(value)?.[1];
    return position ? t("workspace.surveys.edit.picture_idx", { idx: Number(position) }) : value;
  }
  if (!CONSTANT_VALUE_ROW_TYPES.has(type ?? "")) return value;
  const label = Object.hasOwn(FILTER_VALUE_LABELS, value) ? FILTER_VALUE_LABELS[value] : undefined;
  return label ? label(t) : value;
};

/** The label of what a filter row filters on. Only the language row's label is a wire value ("Language", the others key); every other target is named by the survey or the workspace. */
export const getFilterTargetLabel = (
  target: { type?: OptionsType; id?: string; label?: string },
  t: TFunction
): string => {
  if (target.type === OptionsType.OTHERS && target.id === "language") return t("common.language");
  return target.label ?? "";
};

/** The type a filter row's operator and value belong to: the element type for an element row, the OptionsType group otherwise. */
export const getFilterRowType = (row: FilterValue): TFilterRowType =>
  row.elementType.type === OptionsType.ELEMENTS ? row.elementType.elementType : row.elementType.type;

// The text operators, whose value is text someone typed, never one of the fixed values above.
const TEXT_OPERATORS = new Set([
  "Contains",
  "Does not contain",
  "Starts with",
  "Does not start with",
  "Ends with",
  "Does not end with",
]);

/** What one applied condition reads as on its chip: the operator and the value, shown after the target, each through its label. Typed text is quoted, so "Contains Skipped" reads as the word someone searched for. */
export const describeFilterCondition = (row: FilterValue, t: TFunction): string => {
  const rowType = getFilterRowType(row);
  const operator = row.filterType.filterValue;
  const value = row.filterType.filterComboBoxValue;
  const values = Array.isArray(value) ? value : value ? [value] : [];
  const isTypedText = TEXT_OPERATORS.has(operator ?? "");
  return [
    operator ? getFilterOperatorLabel(rowType, operator, t) : "",
    values.map((v) => (isTypedText ? `"${v}"` : getFilterValueLabel(rowType, v, t))).join(", "),
  ]
    .filter(Boolean)
    .join(" ");
};

/** The values 1 to range of a rating or CES scale, as the filter's value strings. */
const scaleValues = (range: number): string[] => Array.from({ length: range }, (_, i) => String(i + 1));

/** The entry an element type has in one of the option tables above, if any. */
const optionsFor = (table: Record<string, string[]>, type: string): string[] | undefined =>
  Object.hasOwn(table, type) ? table[type] : undefined;

// Helper function to get filter options for a specific element type
const getElementFilterOption = (
  element: ReturnType<typeof getElementsFromBlocks>[number]
): ElementFilterOptions | null => {
  const conditions = optionsFor(conditionOptions, element.type);
  if (!conditions) {
    return null;
  }

  const baseOption = {
    type: element.type,
    filterOptions: conditions,
    id: element.id,
  };

  switch (element.type) {
    case TSurveyElementTypeEnum.MultipleChoiceSingle:
      return {
        ...baseOption,
        filterComboBoxOptions: element.choices?.map((c) => c.label) ?? [""],
      };
    case TSurveyElementTypeEnum.MultipleChoiceMulti:
      return {
        ...baseOption,
        filterComboBoxOptions: element.choices?.filter((c) => c.id !== "other").map((c) => c.label) ?? [""],
      };
    case TSurveyElementTypeEnum.PictureSelection:
      return {
        ...baseOption,
        filterComboBoxOptions: element.choices?.map((_, idx) => pictureFilterValue(idx + 1)) ?? [""],
      };
    case TSurveyElementTypeEnum.Rating:
    case TSurveyElementTypeEnum.CES:
      return { ...baseOption, filterComboBoxOptions: scaleValues(element.range) };
    case TSurveyElementTypeEnum.Matrix:
      return {
        type: element.type,
        filterOptions: element.rows.map((row) => getLocalizedValue(row.label, "default")),
        filterComboBoxOptions: element.columns.map((column) => getLocalizedValue(column.label, "default")),
        id: element.id,
      };
    default:
      return {
        ...baseOption,
        filterComboBoxOptions: optionsFor(filterOptions, element.type) ?? [],
      };
  }
};

// URL/meta text operators mapping
const META_OP_MAP = {
  Equals: "equals",
  "Not equals": "notEquals",
  Contains: "contains",
  "Does not contain": "doesNotContain",
  "Starts with": "startsWith",
  "Does not start with": "doesNotStartWith",
  "Ends with": "endsWith",
  "Does not end with": "doesNotEndWith",
} as const;

/** Operators that take no right-hand value; rows carrying them must survive the empty-value cleanup. */
export const NO_VALUE_FILTER_OPERATORS = ["Is set", "Is not set"];

// The score elements, whose Submitted and Skipped operators take no value: the operator alone is the condition (getFormattedFilters sends {op: "submitted"} / {op: "skipped"}).
const SCORE_ELEMENT_TYPES = new Set<string>([
  TSurveyElementTypeEnum.NPS,
  TSurveyElementTypeEnum.Rating,
  TSurveyElementTypeEnum.CSAT,
  TSurveyElementTypeEnum.CES,
  TSurveyElementTypeEnum.Slider,
]);

/** Whether a filter row with this type and operator is complete without a value: "Is set" / "Is not set" on any field, "Submitted" / "Skipped" on a score element. The value box is disabled for these, and the popover's empty-value cleanup must keep them. */
export const isNoValueFilterOperator = (type: TFilterRowType, operator: string | undefined): boolean =>
  NO_VALUE_FILTER_OPERATORS.includes(operator ?? "") ||
  (SCORE_ELEMENT_TYPES.has(type ?? "") && (operator === "Submitted" || operator === "Skipped"));

// Operator menus per Embedded Data / reserved field dataType (ENG-1848) — the editor's logic-builder model: string → equality + text ops, number → comparisons, date → before/after, boolean → equality; every family can also match on absence.
const TEXT_FIELD_OPERATORS = [...Object.keys(META_OP_MAP), ...NO_VALUE_FILTER_OPERATORS];
const NUMBER_FIELD_OPERATORS = [
  "Equals",
  "Not equals",
  "Is greater than",
  "Is less than",
  ...NO_VALUE_FILTER_OPERATORS,
];
const DATE_FIELD_OPERATORS = ["Equals", "Not equals", "Is before", "Is after", ...NO_VALUE_FILTER_OPERATORS];
const BOOLEAN_FIELD_OPERATORS = ["Equals", "Not equals", ...NO_VALUE_FILTER_OPERATORS];

const getTypedFieldOperators = (dataType: TEmbeddedDataType): string[] => {
  switch (dataType) {
    case "number":
      return NUMBER_FIELD_OPERATORS;
    case "date":
      return DATE_FIELD_OPERATORS;
    case "boolean":
      return BOOLEAN_FIELD_OPERATORS;
    default:
      return TEXT_FIELD_OPERATORS;
  }
};

// Operator label → criteria op for the typed field groups. "Is before"/"Is after" reuse the comparison ops: jsonb compares same-format ISO strings lexicographically, i.e. chronologically.
const TYPED_FIELD_OP_MAP: Record<string, string> = {
  ...META_OP_MAP,
  "Is greater than": "greaterThan",
  "Is less than": "lessThan",
  "Is before": "lessThan",
  "Is after": "greaterThan",
  "Is set": "isSet",
  "Is not set": "isNotSet",
};

// Contact attributes are typed string, number or date (contact_attribute_keys.data_type). A date attribute is compared by its day, so "Is on" replaces equality.
const CONTACT_DATE_OPERATORS = ["Is on", "Is before", "Is after", ...NO_VALUE_FILTER_OPERATORS];

/** The operators a contact attribute of this type offers; an attribute whose type is unknown (its key since deleted) is text. */
export const getContactAttributeOperators = (dataType: TContactAttributeDataType | undefined): string[] => {
  switch (dataType) {
    case "number":
      return NUMBER_FIELD_OPERATORS;
    case "date":
      return CONTACT_DATE_OPERATORS;
    default:
      return TEXT_FIELD_OPERATORS;
  }
};

const CONTACT_ATTRIBUTE_OP_MAP: Record<string, string> = {
  ...META_OP_MAP,
  "Is greater than": "greaterThan",
  "Is less than": "lessThan",
  "Is on": "isSameDay",
  "Is before": "isBefore",
  "Is after": "isAfter",
  "Is set": "isSet",
  "Is not set": "isNotSet",
};

type TContactAttributeFilterCondition = Extract<
  NonNullable<TResponseFilterCriteria["contactAttributes"]>[string],
  { op: string }
>;

/** One contact attribute row → one condition: a number attribute sends a real number, which the server compares numerically against the text the response's snapshot holds; a date attribute sends the picked day. A row without a type (a link from before attributes were typed) sends text, except that a comparison sends its value as a number, since only a number attribute offers one. Null when the row cannot form a valid condition. */
const buildContactAttributeCondition = (
  filterType: FilterValue["filterType"],
  dataType: TContactAttributeDataType | undefined
): TContactAttributeFilterCondition | null => {
  const op = CONTACT_ATTRIBUTE_OP_MAP[filterType.filterValue ?? ""];
  if (!op) return null;
  if (op === "isSet" || op === "isNotSet") return { op };

  const raw = Array.isArray(filterType.filterComboBoxValue)
    ? filterType.filterComboBoxValue[0]
    : filterType.filterComboBoxValue;
  if (typeof raw !== "string" || raw.trim().length === 0) return null;
  const value = raw.trim();

  if (op === "isSameDay" || op === "isBefore" || op === "isAfter") {
    return /^\d{4}-\d{2}-\d{2}$/.test(value) ? ({ op, value } as TContactAttributeFilterCondition) : null;
  }
  const isComparison = op === "greaterThan" || op === "lessThan";
  if (dataType === "number" || (dataType === undefined && isComparison)) {
    const numeric = Number(value);
    if (!Number.isFinite(numeric)) return null;
    return { op, value: numeric } as TContactAttributeFilterCondition;
  }
  if (isComparison) return null;
  return { op, value } as TContactAttributeFilterCondition;
};

type TTypedFieldFilterCondition = Extract<
  NonNullable<TResponseFilterCriteria["reserved"]>[string],
  { op: string }
>;

/**
 * One filter row → one typed condition, coerced to the field's dataType (a number field must send a real number — a string-typed `equals` never matches a jsonb number). Returns null when the row cannot form a valid condition, so half-filled rows drop instead of matching wrongly.
 */
const buildTypedFieldCondition = (
  filterType: FilterValue["filterType"],
  dataType: TEmbeddedDataType
): TTypedFieldFilterCondition | null => {
  const op = TYPED_FIELD_OP_MAP[filterType.filterValue ?? ""];
  if (!op) return null;
  if (op === "isSet" || op === "isNotSet") return { op };

  const raw = Array.isArray(filterType.filterComboBoxValue)
    ? filterType.filterComboBoxValue[0]
    : filterType.filterComboBoxValue;
  if (typeof raw !== "string" || raw.trim().length === 0) return null;
  const value = raw.trim();

  if (dataType === "number") {
    const numeric = Number(value);
    if (!Number.isFinite(numeric)) return null;
    return { op, value: numeric } as TTypedFieldFilterCondition;
  }
  if (dataType === "boolean") {
    if (op !== "equals" && op !== "notEquals") return null;
    if (value !== "true" && value !== "false") return null;
    return { op, value: value === "true" } as TTypedFieldFilterCondition;
  }
  return { op, value } as TTypedFieldFilterCondition;
};

export const generateElementAndFilterOptions = ({
  survey,
  environmentTags,
  attributes,
  attributeTypes = {},
  reservedValues,
  hiddenFields,
  variableValues,
  quotas,
  t,
}: {
  survey: TSurvey;
  environmentTags: TTag[] | undefined;
  attributes: TSurveyContactAttributes;
  /** The attribute keys' data types, by key; a key without one is text. */
  attributeTypes?: Record<string, string>;
  reservedValues: TSurveyMetaFieldFilter;
  hiddenFields: TResponseHiddenFieldsFilter;
  variableValues: TSurveyMetaFieldFilter;
  quotas: TSurveyQuota[];
  t: TFunction;
}): {
  elementOptions: ElementOptions[];
  elementFilterOptions: ElementFilterOptions[];
} => {
  let elementOptions: ElementOptions[] = [];
  let elementFilterOptions: ElementFilterOptions[] = [];
  let elementsOptions: ElementOption[] = [];

  const elements = getElementsFromBlocks(survey.blocks);

  elements.forEach((q) => {
    if (Object.keys(conditionOptions).includes(q.type)) {
      elementsOptions.push({
        label: getTextContent(
          getLocalizedValue(recallToHeadline(q.headline, survey, false, "default"), "default")
        ),
        elementType: q.type,
        type: OptionsType.ELEMENTS,
        id: q.id,
      });
    }
  });
  elementOptions = [...elementOptions, { header: OptionsType.ELEMENTS, option: elementsOptions }];
  elements.forEach((q) => {
    const filterOption = getElementFilterOption(q);
    if (filterOption) {
      elementFilterOptions.push(filterOption);
    }
  });

  const tagsOptions = environmentTags?.map((t) => {
    return { label: t.name, type: OptionsType.TAGS, id: t.id };
  });
  if (tagsOptions && tagsOptions?.length > 0) {
    elementOptions = [...elementOptions, { header: OptionsType.TAGS, option: tagsOptions }];
    environmentTags?.forEach((t) => {
      elementFilterOptions.push({
        type: "Tags",
        filterOptions: conditionOptions.tags,
        filterComboBoxOptions: filterOptions.tags,
        id: t.id,
      });
    });
  }

  if (attributes) {
    const attributeType = (key: string): TContactAttributeDataType => {
      const dataType = attributeTypes[key];
      return dataType === "number" || dataType === "date" ? dataType : "string";
    };
    elementOptions = [
      ...elementOptions,
      {
        header: OptionsType.ATTRIBUTES,
        option: Object.keys(attributes).map((a) => {
          return { label: a, type: OptionsType.ATTRIBUTES, id: a, dataType: attributeType(a) };
        }),
      },
    ];
    // Operators per type; a string attribute picks from the observed values or takes typed text, a number or date one takes a typed value.
    Object.keys(attributes).forEach((a) => {
      const dataType = attributeType(a);
      elementFilterOptions.push({
        type: "Attributes",
        filterOptions: getContactAttributeOperators(dataType),
        filterComboBoxOptions: dataType === "string" ? (attributes[a] ?? []) : [],
        fieldDataType: dataType,
        id: a,
      });
    });
  }

  // Ingested embedded fields enumerate from the survey's rows, not from observed response values — a declared field is filterable before its first response. Values live at data[storageKey].
  const ingestedFields = getIngestedEmbeddedFields(survey);
  if (ingestedFields.length > 0) {
    elementOptions = [
      ...elementOptions,
      {
        header: OptionsType.HIDDEN_FIELDS,
        option: ingestedFields.map(({ field, link }) => {
          return { label: field.name, type: OptionsType.HIDDEN_FIELDS, id: link.storageKey };
        }),
      },
    ];
    ingestedFields.forEach(({ field, link }) => {
      elementFilterOptions.push({
        type: "Hidden Fields",
        filterOptions: getTypedFieldOperators(field.dataType),
        filterComboBoxOptions:
          field.dataType === "boolean" ? ["true", "false"] : (hiddenFields[link.storageKey] ?? []),
        fieldDataType: field.dataType,
        id: link.storageKey,
      });
    });
  }

  // Computed embedded fields (variables), keyed by storageKey (ENG-1848).
  const computedFields = getComputedEmbeddedFields(survey);
  if (computedFields.length > 0) {
    elementOptions = [
      ...elementOptions,
      {
        header: OptionsType.VARIABLES,
        option: computedFields.map(({ field, link }) => {
          return { label: field.name, type: OptionsType.VARIABLES, id: link.storageKey };
        }),
      },
    ];
    computedFields.forEach(({ field, link }) => {
      elementFilterOptions.push({
        type: "Variables",
        filterOptions: getTypedFieldOperators(field.dataType),
        filterComboBoxOptions:
          field.dataType === "boolean" ? ["true", "false"] : (variableValues[link.storageKey] ?? []),
        fieldDataType: field.dataType,
        id: link.storageKey,
      });
    });
  }

  // Reserved fields, catalog-driven (ENG-1848), listed after the survey's own declared fields so Hidden Fields and Variables sit above Meta in the picker: the same per-survey list the response table shows (shadowed / anonymized / uncaptured entries gated out by getReservedFilterEntries), with the table's localized labels and operators per dataType — no longer whatever meta keys the stored responses happened to hold.
  const reservedEntries = getReservedFilterEntries(survey);
  if (reservedEntries.length > 0) {
    elementOptions = [
      ...elementOptions,
      {
        header: OptionsType.META,
        option: reservedEntries.map((entry) => {
          return { label: getReservedFieldLabel(entry.name, t), type: OptionsType.META, id: entry.name };
        }),
      },
    ];
    reservedEntries.forEach((entry) => {
      elementFilterOptions.push({
        type: "Meta",
        filterOptions: getTypedFieldOperators(entry.dataType),
        filterComboBoxOptions: reservedValues[entry.name] ?? [],
        fieldDataType: entry.dataType,
        id: entry.name,
      });
    });
  }

  let languageElement: ElementOption[] = [];

  // can be extended to include more properties
  if (survey.languages?.length > 0) {
    languageElement.push({ label: "Language", type: OptionsType.OTHERS, id: "language" });
    const languageOptions = survey.languages.map((sl) => sl.language.code);
    elementFilterOptions.push({
      type: OptionsType.OTHERS,
      filterOptions: conditionOptions.languages,
      filterComboBoxOptions: languageOptions,
      id: "language",
    });
  }
  elementOptions = [...elementOptions, { header: OptionsType.OTHERS, option: languageElement }];

  if (quotas.length > 0) {
    const quotaOptions = quotas.map((quota) => {
      return { label: quota.name, type: OptionsType.QUOTAS, id: quota.id };
    });
    elementOptions = [...elementOptions, { header: OptionsType.QUOTAS, option: quotaOptions }];

    quotas.forEach((quota) => {
      elementFilterOptions.push({
        type: "Quotas",
        filterOptions: ["Status"],
        filterComboBoxOptions: ["Screened in", "Screened out (overquota)", "Not in quota"],
        id: quota.id,
      });
    });
  }

  return { elementOptions: [...elementOptions], elementFilterOptions: [...elementFilterOptions] };
};

/**
 * Adds a condition on key to one of the criteria's keyed groups. A key's first condition is sent as an object, the shape the server has always read; a second condition on the same key (a row using a field again, as a range does) turns it into an array, all of whose conditions must hold.
 */
const addCondition = <C extends { op: string }>(
  group: Record<string, C | C[]>,
  key: string,
  condition: NoInfer<C>
) => {
  const existing = group[key];
  group[key] =
    existing === undefined ? condition : [...(Array.isArray(existing) ? existing : [existing]), condition];
};

type TDataGroup = NonNullable<TResponseFilterCriteria["data"]>;

// Helper function to process filled out/skipped filters
const processFilledOutSkippedFilter = (
  filterType: FilterValue["filterType"],
  elementId: string,
  data: TDataGroup
) => {
  if (filterType.filterComboBoxValue === "Filled out") {
    addCondition(data, elementId, { op: "filledOut" });
  } else if (filterType.filterComboBoxValue === "Skipped") {
    addCondition(data, elementId, { op: "skipped" });
  }
};

// An open text answer: "is" Filled out / Skipped, or Contains / Does not contain typed text, which the server matches ignoring case.
const processOpenTextFilter = (
  filterType: FilterValue["filterType"],
  elementId: string,
  data: TDataGroup
) => {
  const op =
    filterType.filterValue === "Contains"
      ? "contains"
      : filterType.filterValue === "Does not contain"
        ? "doesNotContain"
        : undefined;
  if (!op) {
    processFilledOutSkippedFilter(filterType, elementId, data);
    return;
  }
  const value =
    typeof filterType.filterComboBoxValue === "string" ? filterType.filterComboBoxValue.trim() : "";
  if (value) addCondition(data, elementId, { op, value });
};

// Helper function to process ranking filters
const processRankingFilter = (filterType: FilterValue["filterType"], elementId: string, data: TDataGroup) => {
  if (filterType.filterComboBoxValue === "Filled out") {
    addCondition(data, elementId, { op: "submitted" });
  } else if (filterType.filterComboBoxValue === "Skipped") {
    addCondition(data, elementId, { op: "skipped" });
  }
};

// Helper function to process multiple choice filters
const processMultipleChoiceFilter = (
  filterType: FilterValue["filterType"],
  elementId: string,
  data: TDataGroup
) => {
  if (filterType.filterValue === "Includes either") {
    addCondition(data, elementId, {
      op: "includesOne",
      value: filterType.filterComboBoxValue as string[],
    });
  } else if (filterType.filterValue === "Includes all") {
    addCondition(data, elementId, {
      op: "includesAll",
      value: filterType.filterComboBoxValue as string[],
    });
  }
};

// Helper function to process NPS/Rating filters
const processNPSRatingFilter = (
  filterType: FilterValue["filterType"],
  elementId: string,
  data: TDataGroup
) => {
  if (filterType.filterValue === "Is equal to") {
    addCondition(data, elementId, {
      op: "equals",
      value: parseInt(filterType.filterComboBoxValue as string),
    });
  } else if (filterType.filterValue === "Is less than") {
    addCondition(data, elementId, {
      op: "lessThan",
      value: parseInt(filterType.filterComboBoxValue as string),
    });
  } else if (filterType.filterValue === "Is more than") {
    addCondition(data, elementId, {
      op: "greaterThan",
      value: parseInt(filterType.filterComboBoxValue as string),
    });
  } else if (filterType.filterValue === "Submitted") {
    addCondition(data, elementId, { op: "submitted" });
  } else if (filterType.filterValue === "Skipped") {
    addCondition(data, elementId, { op: "skipped" });
  } else if (filterType.filterValue === "Includes either") {
    addCondition(data, elementId, {
      op: "includesOne",
      value: (filterType.filterComboBoxValue as string[]).map((value) => parseInt(value)),
    });
  }
};

// A slider's value is any number of its scale, decimals included, so it is read whole rather than as an integer.
const processSliderFilter = (filterType: FilterValue["filterType"], elementId: string, data: TDataGroup) => {
  const value = Number(filterType.filterComboBoxValue);
  const hasValue =
    typeof filterType.filterComboBoxValue === "string" && filterType.filterComboBoxValue.trim() !== "";
  if (filterType.filterValue === "Submitted") {
    addCondition(data, elementId, { op: "submitted" });
  } else if (filterType.filterValue === "Skipped") {
    addCondition(data, elementId, { op: "skipped" });
  } else if (hasValue && Number.isFinite(value)) {
    const op =
      filterType.filterValue === "Is equal to"
        ? "equals"
        : filterType.filterValue === "Is less than"
          ? "lessThan"
          : filterType.filterValue === "Is more than"
            ? "greaterThan"
            : undefined;
    if (op) addCondition(data, elementId, { op, value });
  }
};

// Helper function to process CTA filters
const processCTAFilter = (filterType: FilterValue["filterType"], elementId: string, data: TDataGroup) => {
  if (filterType.filterComboBoxValue === "Clicked") {
    addCondition(data, elementId, { op: "clicked" });
  } else if (filterType.filterComboBoxValue === "Dismissed") {
    addCondition(data, elementId, { op: "skipped" });
  }
};

// Helper function to process Consent filters
const processConsentFilter = (filterType: FilterValue["filterType"], elementId: string, data: TDataGroup) => {
  if (filterType.filterComboBoxValue === "Accepted") {
    addCondition(data, elementId, { op: "accepted" });
  } else if (filterType.filterComboBoxValue === "Dismissed") {
    addCondition(data, elementId, { op: "skipped" });
  }
};

// Helper function to process Picture Selection filters
const processPictureSelectionFilter = (
  filterType: FilterValue["filterType"],
  elementId: string,
  element: ReturnType<typeof getElementsFromBlocks>[number] | undefined,
  data: TDataGroup
) => {
  if (
    element?.type !== TSurveyElementTypeEnum.PictureSelection ||
    !Array.isArray(filterType.filterComboBoxValue)
  ) {
    return;
  }

  const selectedOptions = filterType.filterComboBoxValue
    .map((option) => {
      const index = Number.parseInt(option.split(" ")[1] ?? "", 10);
      return element?.choices[index - 1]?.id;
    })
    .filter((id): id is string => Boolean(id));

  if (filterType.filterValue === "Includes all") {
    addCondition(data, elementId, { op: "includesAll", value: selectedOptions });
  } else if (filterType.filterValue === "Includes either") {
    addCondition(data, elementId, { op: "includesOne", value: selectedOptions });
  }
};

// Helper function to process Matrix filters
const processMatrixFilter = (filterType: FilterValue["filterType"], elementId: string, data: TDataGroup) => {
  if (
    filterType.filterValue &&
    filterType.filterComboBoxValue &&
    typeof filterType.filterComboBoxValue === "string"
  ) {
    addCondition(data, elementId, {
      op: "matrix",
      value: { [filterType.filterValue]: filterType.filterComboBoxValue },
    });
  }
};

// Helper function to process element filters
const processElementFilters = (
  elements: FilterValue[],
  survey: TSurvey,
  filters: TResponseFilterCriteria
) => {
  if (!elements.length) return;

  const surveyElements = getElementsFromBlocks(survey.blocks);
  const data = (filters.data = filters.data || {});

  elements.forEach(({ filterType, elementType }) => {
    const elementId = elementType.id ?? "";
    const element = surveyElements.find((q) => q.id === elementId);

    switch (elementType.elementType) {
      case TSurveyElementTypeEnum.OpenText:
        processOpenTextFilter(filterType, elementId, data);
        break;
      case TSurveyElementTypeEnum.Address:
      case TSurveyElementTypeEnum.ContactInfo:
        processFilledOutSkippedFilter(filterType, elementId, data);
        break;
      case TSurveyElementTypeEnum.Ranking:
        processRankingFilter(filterType, elementId, data);
        break;
      case TSurveyElementTypeEnum.MultipleChoiceSingle:
      case TSurveyElementTypeEnum.MultipleChoiceMulti:
        processMultipleChoiceFilter(filterType, elementId, data);
        break;
      case TSurveyElementTypeEnum.NPS:
      case TSurveyElementTypeEnum.Rating:
      case TSurveyElementTypeEnum.CSAT:
      case TSurveyElementTypeEnum.CES:
        processNPSRatingFilter(filterType, elementId, data);
        break;
      case TSurveyElementTypeEnum.Slider:
        processSliderFilter(filterType, elementId, data);
        break;
      case TSurveyElementTypeEnum.CTA:
        processCTAFilter(filterType, elementId, data);
        break;
      case TSurveyElementTypeEnum.Consent:
        processConsentFilter(filterType, elementId, data);
        break;
      case TSurveyElementTypeEnum.PictureSelection:
        processPictureSelectionFilter(filterType, elementId, element, data);
        break;
      case TSurveyElementTypeEnum.Matrix:
        processMatrixFilter(filterType, elementId, data);
        break;
    }
  });
};

// Contact attribute rows → the `contactAttributes` group, each by its attribute's type. A row from before attributes were typed (a saved link) carries no type until the filter popover applies it with the type the filter data reports; until then it is text, which is what it filtered as then.
const processContactAttributeFilters = (attributes: FilterValue[], filters: TResponseFilterCriteria) => {
  attributes.forEach(({ filterType, elementType }) => {
    const key = elementType.id ?? elementType.label;
    if (!key) return;
    const condition = buildContactAttributeCondition(filterType, elementType.dataType);
    if (!condition) return;
    addCondition((filters.contactAttributes ??= {}), key, condition);
  });
};

// The language row → the `others` group.
const processOtherFilters = (others: FilterValue[], filters: TResponseFilterCriteria) => {
  others.forEach(({ filterType, elementType }) => {
    if (!filterType.filterComboBoxValue) return;
    const value = filterType.filterComboBoxValue as string;
    const op =
      filterType.filterValue === "Equals"
        ? "equals"
        : filterType.filterValue === "Not equals"
          ? "notEquals"
          : undefined;
    if (!op) return;
    addCondition((filters.others ??= {}), elementType.label ?? "", { op, value });
  });
};

// Reserved-field filter rows → the `reserved` criteria group, keyed by catalog name (ENG-1848).
const processReservedFilters = (
  reserved: FilterValue[],
  survey: TSurvey,
  filters: TResponseFilterCriteria
) => {
  if (!reserved.length) return;

  const entriesByName = new Map(getReservedFilterEntries(survey).map((entry) => [entry.name, entry]));

  reserved.forEach(({ filterType, elementType }) => {
    const entry = entriesByName.get(elementType.id ?? "");
    if (!entry) return; // fail closed: shadowed/gated names never emit a reserved condition

    const condition = buildTypedFieldCondition(filterType, entry.dataType);
    if (!condition) return;
    addCondition((filters.reserved ??= {}), entry.name, condition);
  });
};

// Computed-field filter rows → the `variables` criteria group, keyed by storageKey (ENG-1848).
const processVariableFilters = (
  variables: FilterValue[],
  survey: TSurvey,
  filters: TResponseFilterCriteria
) => {
  if (!variables.length) return;

  const fieldsByKey = new Map(
    getComputedEmbeddedFields(survey).map((field) => [field.link.storageKey, field])
  );

  variables.forEach(({ filterType, elementType }) => {
    const field = fieldsByKey.get(elementType.id ?? "");
    if (!field) return;

    const condition = buildTypedFieldCondition(filterType, field.field.dataType);
    if (!condition) return;
    addCondition((filters.variables ??= {}), field.link.storageKey, condition);
  });
};

// Ingested-field filter rows → the `data` group under the storageKey. Presence maps onto the data group's own vocabulary: submitted (key present) / skipped (absent or empty), matching isSet's treatment of "" as not set.
const processIngestedFilters = (
  hiddenFields: FilterValue[],
  survey: TSurvey,
  filters: TResponseFilterCriteria
) => {
  if (!hiddenFields.length) return;

  const fieldsByKey = new Map(
    getIngestedEmbeddedFields(survey).map((field) => [field.link.storageKey, field])
  );

  hiddenFields.forEach(({ filterType, elementType }) => {
    const field = fieldsByKey.get(elementType.id ?? "");
    if (!field) return;

    const condition = buildTypedFieldCondition(filterType, field.field.dataType);
    if (!condition) return;
    const data = (filters.data ??= {});
    if (condition.op === "isSet") {
      addCondition(data, field.link.storageKey, { op: "submitted" });
    } else if (condition.op === "isNotSet") {
      addCondition(data, field.link.storageKey, { op: "skipped" });
    } else {
      addCondition(data, field.link.storageKey, condition);
    }
  });
};

// Helper function to process quota filters
const processQuotaFilters = (quotas: FilterValue[], filters: TResponseFilterCriteria) => {
  if (!quotas.length) return;

  const quotaGroup = (filters.quotas = filters.quotas || {});

  const statusMap: Record<string, "screenedIn" | "screenedOut" | "screenedOutNotInQuota"> = {
    "Screened in": "screenedIn",
    "Screened out (overquota)": "screenedOut",
    "Not in quota": "screenedOutNotInQuota",
  };

  quotas.forEach(({ filterType, elementType }) => {
    const quotaId = elementType.id;
    if (!quotaId) return;

    const op = statusMap[String(filterType.filterComboBoxValue)];
    if (op) addCondition(quotaGroup, quotaId, { op });
  });
};

// get the formatted filter expression to fetch filtered responses
export const getFormattedFilters = (
  survey: TSurvey,
  selectedFilter: SelectedFilterValue,
  dateRange: DateRange
): TResponseFilterCriteria => {
  const filters: TResponseFilterCriteria = {};

  const elements: FilterValue[] = [];
  const tags: FilterValue[] = [];
  const attributes: FilterValue[] = [];
  const others: FilterValue[] = [];
  const meta: FilterValue[] = [];
  const hiddenFields: FilterValue[] = [];
  const variables: FilterValue[] = [];
  const quotas: FilterValue[] = [];

  selectedFilter.filter.forEach((filter) => {
    if (filter.elementType?.type === "Elements") {
      elements.push(filter);
    } else if (filter.elementType?.type === "Tags") {
      tags.push(filter);
    } else if (filter.elementType?.type === "Attributes") {
      attributes.push(filter);
    } else if (filter.elementType?.type === "Other Filters") {
      others.push(filter);
    } else if (filter.elementType?.type === "Meta") {
      meta.push(filter);
    } else if (filter.elementType?.type === "Hidden Fields") {
      hiddenFields.push(filter);
    } else if (filter.elementType?.type === "Variables") {
      variables.push(filter);
    } else if (filter.elementType?.type === "Quotas") {
      quotas.push(filter);
    }
  });

  // for completed responses
  if (selectedFilter.responseStatus === "complete") {
    filters["finished"] = true;
  } else if (selectedFilter.responseStatus === "partial") {
    filters["finished"] = false;
  }

  // for date range responses
  if (dateRange.from && dateRange.to) {
    filters["createdAt"] = {
      min: dateRange.from,
      max: dateRange.to,
    };
  }

  // for tags
  if (tags.length) {
    filters["tags"] = {
      applied: [],
      notApplied: [],
    };
    tags.forEach((tag) => {
      if (tag.filterType.filterComboBoxValue === "Applied") {
        filters.tags?.applied?.push(tag.elementType.label ?? "");
      } else {
        filters.tags?.notApplied?.push(tag.elementType.label ?? "");
      }
    });
  }

  processElementFilters(elements, survey, filters);
  processIngestedFilters(hiddenFields, survey, filters);
  processVariableFilters(variables, survey, filters);
  processContactAttributeFilters(attributes, filters);
  processOtherFilters(others, filters);
  processReservedFilters(meta, survey, filters);
  processQuotaFilters(quotas, filters);

  // The keyword search above the response table.
  const keyword = selectedFilter.keyword?.trim();
  if (keyword) filters.search = keyword;

  return filters;
};

// get the today date with full hours
export const getTodayDate = (): Date => {
  const date = new Date();
  date.setHours(23, 59, 59, 999);
  return date;
};
