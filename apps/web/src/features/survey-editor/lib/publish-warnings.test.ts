import { describe, expect, test } from "vite-plus/test";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type { TSurvey } from "@forma/types/surveys/types";
import catalogEnUS from "../../../../../server/internal/survey/catalog/locales/en-US.json";
import catalogJaJP from "../../../../../server/internal/survey/catalog/locales/ja-JP.json";
import catalogZhHansCN from "../../../../../server/internal/survey/catalog/locales/zh-Hans-CN.json";
import catalogZhHantTW from "../../../../../server/internal/survey/catalog/locales/zh-Hant-TW.json";
import { getPublishWarnings } from "./publish-warnings";

const element = (id: string, headline: Record<string, string>, subheader?: Record<string, string>) => ({
  id,
  type: TSurveyElementTypeEnum.OpenText,
  headline,
  subheader,
  required: false,
  inputType: "text" as const,
  charLimit: { enabled: false },
});

const surveyWith = (overrides: Partial<Record<"blocks" | "endings" | "welcomeCard", unknown>>): TSurvey =>
  ({
    welcomeCard: { enabled: false },
    blocks: [],
    endings: [],
    ...overrides,
  }) as unknown as TSurvey;

const surveyAsking = (headline: string): TSurvey =>
  surveyWith({
    blocks: [{ id: "block1", name: "Block 1", elements: [element("q1", { default: headline })] }],
  });

describe("getPublishWarnings", () => {
  test("a survey written for the product warns about nothing", () => {
    expect(getPublishWarnings(surveyAsking("How likely are you to recommend Acme to a friend?"))).toEqual([]);
  });

  test("the NPS template created in a workspace still named by default warns about the workspace name, on its question", () => {
    expect(
      getPublishWarnings(
        surveyWith({
          blocks: [
            { id: "block1", name: "Block 1", elements: [element("q0", { default: "Hello" })] },
            {
              id: "block2",
              name: "Block 2",
              elements: [
                element("q1", { default: "Hi" }),
                element("q2", {
                  default:
                    '<p class="fb-editor-paragraph" dir="ltr"><span>How likely are you to recommend My workspace to a friend or colleague?</span></p>',
                }),
              ],
            },
          ],
        })
      )
    ).toEqual([
      {
        kind: "defaultWorkspaceName",
        text: "My workspace",
        location: { type: "element", blockNumber: 2, questionNumber: 2 },
        scrollId: "q2",
        fallbackScrollId: "block2",
      },
    ]);
  });

  test("every placeholder left in a headline or subheader, in any language, is named once per card", () => {
    const warnings = getPublishWarnings(
      surveyWith({
        blocks: [
          {
            id: "block1",
            name: "Block 1",
            elements: [
              element(
                "q1",
                { default: "My workspace makes it easy for me to [ADD GOAL]", ja: "[目標を追加]" },
                { default: "Thanks! How could we make it easier for you to [ADD GOAL]?" }
              ),
            ],
          },
        ],
      })
    );
    expect(warnings.map(({ kind, text }) => [kind, text])).toEqual([
      ["defaultWorkspaceName", "My workspace"],
      ["placeholder", "[ADD GOAL]"],
      ["placeholder", "[目標を追加]"],
    ]);
  });

  test("the welcome card and the ending cards respondents see are checked, a redirect is not, and a disabled welcome card is not", () => {
    const endings = [
      {
        id: "redirect",
        type: "redirectToUrl",
        url: "https://example.com/[ADD FEATURE]",
        label: "[ADD FEATURE]",
      },
      {
        id: "ending2",
        type: "endScreen",
        headline: { default: "Thanks" },
        subheader: { default: "[PROBLEM AREA]" },
      },
    ];
    const welcomeCard = { enabled: true, headline: { default: "Welcome to My workspace" } };
    expect(getPublishWarnings(surveyWith({ welcomeCard, endings }))).toEqual([
      {
        kind: "defaultWorkspaceName",
        text: "My workspace",
        location: { type: "welcomeCard" },
        scrollId: "start",
        fallbackScrollId: null,
      },
      {
        kind: "placeholder",
        text: "[PROBLEM AREA]",
        location: { type: "ending", endingNumber: 2 },
        scrollId: "ending2",
        fallbackScrollId: null,
      },
    ]);
    expect(getPublishWarnings(surveyWith({ welcomeCard: { ...welcomeCard, enabled: false } }))).toEqual([]);
  });

  test("a $[workspaceName] the server left unresolved is a placeholder, and author brackets are not", () => {
    expect(
      getPublishWarnings(surveyAsking("Do you like $[workspaceName]? [Optional] answer [A] or [b]")).map(
        ({ text }) => text
      )
    ).toEqual(["$[workspaceName]"]);
  });

  // The template catalogue is where the placeholders come from: a placeholder added to a template, or translated differently, must be caught too.
  test.each([
    ["en-US", catalogEnUS],
    ["ja-JP", catalogJaJP],
    ["zh-Hans-CN", catalogZhHansCN],
    ["zh-Hant-TW", catalogZhHantTW],
  ])("every placeholder in the %s template catalogue is caught", (_locale, catalogue) => {
    const placeholders = new Set(
      Object.values(catalogue as Record<string, string>).flatMap(
        (value) => value.match(/\$?\[[^\]\n]+\]/g) ?? []
      )
    );
    expect(placeholders.size).toBeGreaterThan(1);
    const missed = [...placeholders].filter(
      (placeholder) =>
        !getPublishWarnings(surveyAsking(`Before ${placeholder} after`)).some(
          ({ text }) => text === placeholder
        )
    );
    expect(missed).toEqual([]);
  });
});
