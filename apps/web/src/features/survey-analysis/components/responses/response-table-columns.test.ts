import type { TFunction } from "i18next";
import type { TSurvey } from "@forma/types/surveys/types";
import { describe, expect, test } from "vite-plus/test";
import { generateResponseTableColumns, getDefaultHiddenColumnIds } from "./response-table-columns";

const t = ((key: string) => key) as unknown as TFunction;

// A survey with one choice question and the hidden field `plan`, inlined as the server sends it (embeddedFields).
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

describe("generateResponseTableColumns", () => {
  test("renders a column for each hidden field the survey's embeddedFields declare", () => {
    expect(columnIds(survey)).toContain("HIDDEN_FIELD_plan");
  });

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
