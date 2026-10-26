import type { TFunction } from "i18next";
import { RESERVED_FIELD_CATALOG } from "@forma/types/embedded-data-resolver";
import type { TI18nString } from "@forma/types/i18n";
import { formatFieldNameToTitleCase } from "@forma/types/safe-identifier";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import { getTextContent } from "@forma/types/surveys/validation";
import { replaceRecallInfoWithUnderline } from "../../lib/survey/recall";
import type {
  ElementSelection,
  LinkBase,
  LinkableSurvey,
  NotionDatabase,
  NotionLink,
  NotionMapping,
  NotionRef,
} from "./api";

// The link modals' rules, forma2's integrations AddIntegrationModal / AddChannelMappingModal / MappingRow: what a saved link holds, and how a Notion mapping is checked.

/** The extra columns a link writes, forma2's AdditionalIntegrationSettings. */
export interface LinkSettings {
  includeCreatedAt: boolean;
  includeVariables: boolean;
  includeHiddenFields: boolean;
  includeMetadata: boolean;
  includeContactAttributes: boolean;
}

/** A new link writes the response's creation time and nothing else extra, as forma2's modals started. */
export const DEFAULT_LINK_SETTINGS: LinkSettings = {
  includeCreatedAt: true,
  includeVariables: false,
  includeHiddenFields: false,
  includeMetadata: false,
  includeContactAttributes: false,
};

/** The settings of a saved link; a flag it does not carry is off. */
export const linkSettingsOf = (link: LinkBase): LinkSettings => ({
  includeCreatedAt: !!link.includeCreatedAt,
  includeVariables: !!link.includeVariables,
  includeHiddenFields: !!link.includeHiddenFields,
  includeMetadata: !!link.includeMetadata,
  includeContactAttributes: !!link.includeContactAttributes,
});

/** What every link saves about its survey, stamped now as forma2 stamped createdAt on every save. */
export const linkBase = (
  survey: LinkableSurvey,
  settings: LinkSettings,
  now: Date = new Date()
): LinkBase => ({
  createdAt: now.toISOString(),
  surveyId: survey.id,
  surveyName: survey.name,
  ...settings,
});

/** The element selection of a link, with the label the tables show: "All questions" when every element is selected. */
export const elementSelection = (
  t: TFunction,
  survey: LinkableSurvey,
  elementIds: string[]
): ElementSelection => ({
  elementIds,
  elements:
    elementIds.length === survey.elements.length ? t("common.all_questions") : t("common.selected_questions"),
});

/** The links after saving one: appended when it is new, in its place when an existing one was edited. The array the integration came with is never modified, so a failed save leaves nothing behind for a retry to save twice. */
export const withLink = <T>(links: readonly T[], link: T, index?: number): T[] =>
  index === undefined ? [...links, link] : links.map((existing, i) => (i === index ? link : existing));

/** The links after deleting the one at index. */
export const withoutLink = <T>(links: readonly T[], index: number): T[] =>
  links.filter((_, i) => i !== index);

/** Toggles id in a selection, keeping the order the ids were picked in. */
export const toggled = <T extends string>(ids: readonly T[], id: T): T[] =>
  ids.includes(id) ? ids.filter((value) => value !== id) : [...ids, id];

// ---- labels ----

const RECALL_TOKEN = /#recall:([A-Za-z0-9_-]+)\/fallback:([^#]*)#/g;

/** What a recall token in a headline is called, with forma2's precedence: a hidden field, an element, a variable, then a reserved field. */
const recallLabel = (survey: LinkableSurvey, id: string): string => {
  if (survey.hiddenFields.includes(id)) return id;
  const element = survey.elements.find((el) => el.id === id);
  if (element) return getTextContent(element.headline.default ?? "");
  const variable = survey.variables.find((v) => v.storageKey === id);
  if (variable) return variable.name;
  const reserved = RESERVED_FIELD_CATALOG.find((entry) => entry.name === id);
  if (reserved) return formatFieldNameToTitleCase(reserved.name);
  return id;
};

/** An element's headline as the modals list it: the default language, a recall token shown as @label, as plain text (forma2's getTextContent(recallToHeadline(headline)["default"])). */
export const headlineLabel = (survey: LinkableSurvey, headline: TI18nString): string => {
  const text = headline.default ?? "";
  if (!text.includes("#recall:")) return getTextContent(text);
  const replaced = text.replaceAll(
    RECALL_TOKEN,
    (_, id: string) => `@${replaceRecallInfoWithUnderline(recallLabel(survey, id))}`
  );
  return getTextContent(replaced);
};

// ---- Google Sheets ----

const SPREADSHEETS_URL = "https://docs.google.com/spreadsheets/d/";

/** forma2's isValidGoogleSheetsUrl. */
export const isValidGoogleSheetsUrl = (url: string): boolean => url.startsWith(SPREADSHEETS_URL);

/** forma2's extractSpreadsheetIdFromUrl; throws on a URL without one. */
export const extractSpreadsheetIdFromUrl = (url: string): string => {
  const match = /\/spreadsheets\/d\/([a-zA-Z0-9-_]+)/.exec(url);
  if (match?.[1]) return match[1];
  throw new Error("Invalid Google Sheets URL");
};

/** forma2's constructGoogleSheetsUrl: the URL an edited link shows. */
export const constructGoogleSheetsUrl = (spreadsheetId: string): string => SPREADSHEETS_URL + spreadsheetId;

// ---- Notion ----

/** The Notion property types each element type can be written to, forma2's TYPE_MAPPING. */
export const NOTION_TYPE_MAPPING: Record<string, string[]> = {
  [TSurveyElementTypeEnum.CTA]: ["checkbox"],
  [TSurveyElementTypeEnum.MultipleChoiceMulti]: ["multi_select"],
  [TSurveyElementTypeEnum.MultipleChoiceSingle]: ["select", "status"],
  [TSurveyElementTypeEnum.OpenText]: [
    "created_by",
    "created_time",
    "email",
    "last_edited_by",
    "last_edited_time",
    "number",
    "phone_number",
    "rich_text",
    "title",
    "url",
  ],
  [TSurveyElementTypeEnum.NPS]: ["number"],
  [TSurveyElementTypeEnum.Consent]: ["checkbox"],
  [TSurveyElementTypeEnum.Rating]: ["number"],
  [TSurveyElementTypeEnum.Slider]: ["number"],
  [TSurveyElementTypeEnum.PictureSelection]: ["url"],
  [TSurveyElementTypeEnum.FileUpload]: ["url"],
  [TSurveyElementTypeEnum.Date]: ["date"],
  [TSurveyElementTypeEnum.Address]: ["rich_text"],
  [TSurveyElementTypeEnum.Matrix]: ["rich_text"],
  [TSurveyElementTypeEnum.Cal]: ["checkbox"],
  [TSurveyElementTypeEnum.ContactInfo]: ["rich_text"],
  [TSurveyElementTypeEnum.Ranking]: ["rich_text"],
};

/** Property types Notion's API does not let a page be created with, forma2's UNSUPPORTED_TYPES_BY_NOTION. */
export const NOTION_UNSUPPORTED_TYPES = [
  "rollup",
  "created_by",
  "created_time",
  "last_edited_by",
  "last_edited_time",
];

/** forma2's mapping errors, shown verbatim as the error's heading. */
export const NOTION_MAPPING_ERRORS = {
  MAPPING: "Mapping Error",
  UNSUPPORTED_TYPE: "Unsupported type by Notion",
} as const;

export type NotionMappingError = (typeof NOTION_MAPPING_ERRORS)[keyof typeof NOTION_MAPPING_ERRORS];

/** A mapping row of the modal: an entry being edited, with a key that survives reordering. */
export interface MappingRow extends NotionMapping {
  key: string;
}

const emptyRef = (): NotionRef => ({ id: "", name: "", type: "" });

let rowKey = 0;
export const emptyMappingRow = (): MappingRow => ({
  key: `row-${++rowKey}`,
  element: emptyRef(),
  column: emptyRef(),
});

export const mappingRowsOf = (mapping: readonly NotionMapping[]): MappingRow[] =>
  mapping.map((m) => ({ ...emptyMappingRow(), element: m.element, column: m.column }));

/** What is wrong with a row once both sides are chosen: a property Notion cannot write, or an element that cannot go into a property of that type. */
export const mappingErrorOf = (row: NotionMapping): NotionMappingError | null => {
  if (!row.element.id || !row.column.id) return null;
  if (NOTION_UNSUPPORTED_TYPES.includes(row.column.type)) return NOTION_MAPPING_ERRORS.UNSUPPORTED_TYPE;
  if (!NOTION_TYPE_MAPPING[row.element.type]?.includes(row.column.type)) return NOTION_MAPPING_ERRORS.MAPPING;
  return null;
};

/** What a Notion mapping can write from a survey, in forma2's order: its elements, its variables, its hidden fields, the metadata, the creation time, and each contact attribute. */
export const notionElementItems = (
  t: TFunction,
  survey: LinkableSurvey,
  attributeKeys: readonly { key: string; name: string | null }[]
): NotionRef[] => [
  ...survey.elements.map((el) => ({ id: el.id, name: headlineLabel(survey, el.headline), type: el.type })),
  ...survey.variables.map((v) => ({ id: v.storageKey, name: v.name, type: TSurveyElementTypeEnum.OpenText })),
  ...survey.hiddenFields.map((key) => ({
    id: key,
    name: `${t("common.hidden_field")} : ${key}`,
    type: TSurveyElementTypeEnum.OpenText,
  })),
  { id: "metadata", name: t("common.metadata"), type: TSurveyElementTypeEnum.OpenText },
  { id: "createdAt", name: t("common.created_at"), type: TSurveyElementTypeEnum.Date },
  ...attributeKeys.map((attribute) => ({
    id: `person.${attribute.key}`,
    name: `${t("common.person")}: ${attribute.name ?? attribute.key}`,
    type: TSurveyElementTypeEnum.OpenText,
  })),
];

/** The items a row may pick: everything no other row has picked already. */
export const unusedBy = (
  items: readonly NotionRef[],
  rows: readonly MappingRow[],
  index: number,
  side: "element" | "column"
): NotionRef[] => {
  const taken = new Set(rows.filter((_, i) => i !== index).map((row) => row[side].id));
  return items.filter((item) => !taken.has(item.id));
};

/** forma2's linkDatabase checks, in its order; the first failure's message, or null when the link can be saved. */
export const notionLinkProblem = (
  t: TFunction,
  input: { hasDatabase: boolean; hasSurvey: boolean; rows: readonly MappingRow[] }
): string | null => {
  const { rows } = input;
  if (!input.hasDatabase) return t("workspace.integrations.notion.please_select_a_database");
  if (!input.hasSurvey) return t("workspace.integrations.please_select_a_survey_error");
  if (rows.length === 1 && (!rows[0]?.element.id || !rows[0]?.column.id))
    return t("workspace.integrations.notion.please_select_at_least_one_mapping");
  if (rows.some((row) => mappingErrorOf(row)))
    return t("workspace.integrations.notion.please_resolve_mapping_errors");
  if (rows.some((row) => !!row.column.id !== !!row.element.id))
    return t("workspace.integrations.notion.please_complete_mapping_fields_with_notion_property");
  return null;
};

/** The mapping a link saves: the complete rows without their keys. A row left empty on both sides maps nothing, and the server refuses an entry without an element or a property. */
export const notionMappingOf = (rows: readonly MappingRow[]): NotionMapping[] =>
  rows.filter((row) => row.element.id && row.column.id).map(({ element, column }) => ({ element, column }));

/** The link a Notion modal saves. It names its columns through the mapping, so it carries none of the extra-column flags, as forma2's did not. */
export const notionLink = (
  survey: LinkableSurvey,
  database: Pick<NotionDatabase, "id" | "name">,
  rows: readonly MappingRow[],
  now: Date = new Date()
): NotionLink => ({
  createdAt: now.toISOString(),
  surveyId: survey.id,
  surveyName: survey.name,
  mapping: notionMappingOf(rows),
  databaseId: database.id,
  databaseName: database.name,
});
