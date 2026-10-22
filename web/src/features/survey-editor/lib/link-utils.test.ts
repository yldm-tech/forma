import { describe, expect, test } from "vite-plus/test";
import { TJsWorkspaceStateSurvey } from "@forma/types/js";
import { TSurveyBlock } from "@forma/types/surveys/blocks";
import { TSurveyElement, TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import { getElementsFromSurveyBlocks, isRTL, isRTLLanguage } from "./link-utils";

describe("isRTL", () => {
  test("detects RTL characters", () => {
    expect(isRTL("مرحبا")).toBe(true);
    expect(isRTL("שלום")).toBe(true);
    expect(isRTL("Hello")).toBe(false);
  });
});

describe("isRTLLanguage", () => {
  const createJsSurvey = (
    languages: TJsWorkspaceStateSurvey["languages"] = [],
    blocks: TSurveyBlock[] = []
  ): TJsWorkspaceStateSurvey =>
    ({
      id: "s1",
      createdAt: new Date(),
      updatedAt: new Date(),
      name: "Test",
      type: "link",
      workspaceId: "ws-1",
      welcomeCard: {
        enabled: false,
        headline: { default: "Welcome" },
        timeToFinish: false,
        showResponseCount: false,
      },
      blocks,
      languages,
    }) as unknown as TJsWorkspaceStateSurvey;

  test("checks language codes when multi-language enabled", () => {
    const survey = createJsSurvey([
      {
        language: {
          id: "l1",
          code: "ar",
          alias: null,
          createdAt: new Date(),
          updatedAt: new Date(),
          workspaceId: "p1",
        },
        default: true,
        enabled: true,
      },
    ]);
    expect(isRTLLanguage(survey, "ar")).toBe(true);
    expect(isRTLLanguage(survey, "en")).toBe(false);
  });

  test("checks content when no languages configured", () => {
    const element = {
      id: "q1",
      type: TSurveyElementTypeEnum.OpenText,
      headline: { default: "مرحبا" },
      required: false,
    } as unknown as TSurveyElement;
    const block = { id: "b1", name: "Block", elements: [element] } as TSurveyBlock;
    expect(isRTLLanguage(createJsSurvey([], [block]), "default")).toBe(true);
  });

  test("checks welcomeCard headline when enabled and no languages", () => {
    const survey = {
      ...createJsSurvey([], []),
      welcomeCard: { enabled: true, headline: { default: "مرحبا" } },
    } as unknown as TJsWorkspaceStateSurvey;
    expect(isRTLLanguage(survey, "default")).toBe(true);
  });

  test("returns false when no languages and no headlines found", () => {
    const element = { id: "q1", type: TSurveyElementTypeEnum.OpenText, headline: {}, required: false };
    const block = { id: "b1", name: "Block", elements: [element] } as TSurveyBlock;
    expect(isRTLLanguage(createJsSurvey([], [block]), "default")).toBe(false);
  });
});

describe("getElementsFromSurveyBlocks", () => {
  test("extracts elements from blocks", () => {
    const el1 = {
      id: "q1",
      type: TSurveyElementTypeEnum.OpenText,
      headline: { default: "Q1" },
      required: false,
    } as unknown as TSurveyElement;
    const el2 = {
      id: "q2",
      type: TSurveyElementTypeEnum.OpenText,
      headline: { default: "Q2" },
      required: false,
    } as unknown as TSurveyElement;
    const block = { id: "b1", name: "Block", elements: [el1, el2] } as TSurveyBlock;
    const result = getElementsFromSurveyBlocks([block]);
    expect(result).toHaveLength(2);
    expect(result[0]!.id).toBe("q1");
  });
});
