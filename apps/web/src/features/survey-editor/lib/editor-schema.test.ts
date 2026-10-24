import { describe, expect, test } from "vite-plus/test";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import { type TSurvey, ZSurvey, ZSurveyBase } from "@forma/types/surveys/types";
import { ZEditorSurvey } from "./editor-schema";

const now = new Date("2026-10-01T00:00:00.000Z");

// A survey as the Go server returns it: UUIDs for the records it creates, cuid2 for what the editor creates.
const survey = (): TSurvey =>
  ({
    id: "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0e",
    createdAt: now,
    updatedAt: now,
    name: "Feedback",
    type: "link",
    workspaceId: "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0f",
    createdBy: null,
    status: "draft",
    displayOption: "displayOnce",
    autoClose: null,
    triggers: [],
    recontactDays: null,
    displayLimit: null,
    welcomeCard: { enabled: false, timeToFinish: true, showResponseCount: false },
    questions: [],
    blocks: [
      {
        id: "blockone0000000000000000",
        name: "Block 1",
        elements: [
          {
            id: "q1",
            type: TSurveyElementTypeEnum.OpenText,
            headline: { default: "How was it?", de: "Wie war es?" },
            required: true,
            inputType: "text",
            charLimit: { enabled: false },
          },
        ],
      },
    ],
    endings: [
      { id: "endingone000000000000000", type: "endScreen", headline: { default: "Thanks", de: "Danke" } },
    ],
    hiddenFields: { enabled: true, fieldIds: [] },
    variables: [],
    followUps: [],
    delay: 0,
    publishOn: null,
    closeOn: null,
    autoComplete: null,
    workspaceOverwrites: null,
    styling: null,
    showLanguageSwitch: null,
    surveyClosedMessage: null,
    segment: null,
    singleUse: null,
    isVerifyEmailEnabled: false,
    recaptcha: null,
    isBackButtonHidden: false,
    isAutoProgressingEnabled: false,
    isCaptureIpEnabled: false,
    isAnonymizeResponsesEnabled: false,
    pin: null,
    displayPercentage: null,
    languages: [
      {
        default: true,
        enabled: true,
        language: {
          id: "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d10",
          createdAt: now,
          updatedAt: now,
          code: "en",
          alias: null,
          workspaceId: "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0f",
        },
      },
      {
        default: false,
        enabled: true,
        language: {
          id: "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d11",
          createdAt: now,
          updatedAt: now,
          code: "de",
          alias: null,
          workspaceId: "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0f",
        },
      },
    ],
    metadata: {},
    slug: null,
  }) as TSurvey;

describe("ZEditorSurvey", () => {
  test("accepts the UUIDs the Go server gives the records it creates, which forma2's schema refuses", () => {
    expect(ZSurvey.safeParse(survey()).success).toBe(false);
    expect(ZEditorSurvey.safeParse(survey()).error?.issues).toBeUndefined();
  });

  test("keeps forma2's refinements, such as a block name being required", () => {
    const withoutName = survey();
    withoutName.blocks[0]!.name = "";
    const issues = ZEditorSurvey.safeParse(withoutName).error?.issues ?? [];
    expect(issues.map((issue) => issue.path)).toContainEqual(["blocks", 0, "name"]);
  });

  test("keeps the translation check, naming the language that misses a headline", () => {
    const untranslated = survey();
    untranslated.blocks[0]!.elements[0]!.headline = { default: "How was it?", de: "" };
    const issue = ZEditorSurvey.safeParse(untranslated).error?.issues.find(
      (candidate) => candidate.path.join(".") === "blocks.0.elements.0.headline"
    );
    expect(issue?.code === "custom" ? issue.params : undefined).toEqual({ invalidLanguageCodes: ["de"] });
  });

  test.each(["", "   ", "\t"])(
    "refuses the survey name %j, which forma2's schema let a publish store",
    (name) => {
      const unnamed = { ...survey(), name };
      expect(ZSurveyBase.shape.name.safeParse(name).success).toBe(true);
      const issues = ZEditorSurvey.safeParse(unnamed).error?.issues ?? [];
      expect(issues.map((issue) => issue.path)).toContainEqual(["name"]);
    }
  );

  test("still requires the ids the editor creates to be cuid2", () => {
    const badBlockId = survey();
    badBlockId.blocks[0]!.id = "Not A Cuid";
    expect(ZEditorSurvey.safeParse(badBlockId).success).toBe(false);
  });
});
