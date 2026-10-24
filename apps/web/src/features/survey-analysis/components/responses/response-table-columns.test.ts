import type { TFunction } from "i18next";
import type { TSurvey } from "@forma/types/surveys/types";
import { describe, expect, test } from "vite-plus/test";
import { OptionsType } from "../../lib/filter-types";
import { generateElementAndFilterOptions } from "../../lib/filters";
import { generateResponseTableColumns, getDefaultHiddenColumnIds } from "./response-table-columns";

const t = ((key: string) => key) as unknown as TFunction;

// A survey with one choice question and the hidden field `plan`, inlined as the server sends it (embeddedFields). The web side always read embeddedFields; RA-01 was the server not sending them, which TestEditorSave and TestCopyAndDuplicate in apps/server/internal/survey guard. These cases pin the other half of that contract: the payload shape the server now sends yields the hidden-field column and filter group.
const survey = {
  id: "survey",
  workspaceId: "workspace",
  isVerifyEmailEnabled: false,
  singleUse: { enabled: false, isEncrypted: false },
  blocks: [
    {
      id: "b1",
      name: "Block 1",
      elements: [
        {
          id: "q1",
          type: "multipleChoiceSingle",
          headline: { default: "Plan?" },
          required: true,
          choices: [{ id: "c1", label: { default: "Pro" } }],
        },
      ],
    },
  ],
  hiddenFields: { enabled: true, fieldIds: ["plan"] },
  variables: [],
  embeddedFields: [
    {
      field: { name: "plan", source: "ingested", dataType: "string", defaultValue: null, locked: false },
      link: { storageKey: "plan" },
    },
  ],
} as unknown as TSurvey;

const columnIds = (forSurvey: TSurvey, isReadOnly = false) =>
  generateResponseTableColumns(forSurvey, false, isReadOnly, "en-US", t, false).map((column) =>
    "accessorKey" in column ? String(column.accessorKey) : (column.id ?? "")
  );

describe("the embeddedFields contract with the server", () => {
  test("renders a column for each hidden field the survey's embeddedFields declare", () => {
    expect(columnIds(survey)).toContain("HIDDEN_FIELD_plan");
  });

  test("offers each hidden field in the filter's Hidden Fields group", () => {
    const { elementOptions, elementFilterOptions } = generateElementAndFilterOptions({
      survey,
      environmentTags: undefined,
      attributes: {},
      reservedValues: {},
      hiddenFields: { plan: ["pro"] },
      variableValues: {},
      quotas: [],
      t,
    });
    expect(elementOptions.find((group) => group.header === OptionsType.HIDDEN_FIELDS)?.option).toStrictEqual([
      { label: "plan", type: OptionsType.HIDDEN_FIELDS, id: "plan" },
    ]);
    expect(
      elementFilterOptions.find((option) => option.type === "Hidden Fields" && option.id === "plan")
        ?.filterComboBoxOptions
    ).toStrictEqual(["pro"]);
  });
});

describe("generateResponseTableColumns", () => {
  test("puts the date and the answers before the ids", () => {
    const ids = columnIds(survey);
    expect(ids.slice(0, 5)).toStrictEqual(["select", "createdAt", "personId", "status", "tags"]);
    expect(ids.indexOf("ELEMENT_q1")).toBeLessThan(ids.indexOf("responseId"));
    expect(ids.indexOf("HIDDEN_FIELD_plan")).toBeLessThan(ids.indexOf("singleUseId"));
  });
});

describe("getDefaultHiddenColumnIds", () => {
  test("hides the option ids, and the single-use id unless the survey uses single-use links", () => {
    expect(getDefaultHiddenColumnIds(survey, columnIds(survey))).toStrictEqual([
      "ELEMENT_q1optionIds",
      "singleUseId",
    ]);
    const singleUse = { ...survey, singleUse: { enabled: true, isEncrypted: false } } as TSurvey;
    expect(getDefaultHiddenColumnIds(singleUse, columnIds(singleUse))).toStrictEqual(["ELEMENT_q1optionIds"]);
  });
});
