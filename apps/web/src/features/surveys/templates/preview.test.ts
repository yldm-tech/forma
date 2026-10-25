import { describe, expect, test } from "vite-plus/test";
import type { SurveyTemplate } from "../api";
import { templatePreviewSurvey } from "./preview";

const block = {
  id: "blk-1",
  name: "Block 1",
  elements: [
    { id: "q1", type: "openText", headline: { default: "What could we improve?" }, required: false },
  ],
};

const template = (preset: Record<string, unknown>): SurveyTemplate => ({
  id: "product-feedback",
  name: "Product feedback",
  description: "Ask what to improve.",
  preset: preset as SurveyTemplate["preset"],
});

describe("templatePreviewSurvey", () => {
  test("takes the survey document from the template's preset", () => {
    const welcomeCard = {
      enabled: true,
      headline: { default: "Welcome" },
      timeToFinish: true,
      showResponseCount: false,
    };
    const endings = [{ id: "end-1", type: "endScreen", headline: { default: "Thanks" } }];
    const hiddenFields = { enabled: true, fieldIds: ["source"] };
    const variables = [{ id: "var-1", name: "score", type: "number", value: 0 }];
    const survey = templatePreviewSurvey(
      template({
        name: "Product feedback for Main",
        welcomeCard,
        blocks: [block],
        endings,
        hiddenFields,
        variables,
        metadata: { title: { default: "Feedback" } },
      }),
      "link"
    );
    expect(survey.name).toBe("Product feedback for Main");
    expect(survey.welcomeCard).toEqual(welcomeCard);
    expect(survey.blocks).toEqual([block]);
    expect(survey.endings).toEqual(endings);
    expect(survey.hiddenFields).toEqual(hiddenFields);
    expect(survey.variables).toEqual(variables);
    expect(survey.metadata).toEqual({ title: { default: "Feedback" } });
  });

  test("is a single-language survey of the gallery's type that switches nothing on", () => {
    const survey = templatePreviewSurvey(template({ blocks: [block] }), "app");
    expect(survey.id).toBe("product-feedback");
    expect(survey.type).toBe("app");
    expect(survey.languages).toEqual([]);
    expect(survey.showLanguageSwitch).toBe(false);
    expect(survey.styling).toBeNull();
    expect(survey.workspaceOverwrites).toBeNull();
    expect(survey.recaptcha).toBeNull();
    expect(survey.segment).toBeNull();
    expect(survey.triggers).toEqual([]);
    expect(survey.questions).toEqual([]);
  });

  test("fills what the preset leaves out as a new survey has it, named after the template", () => {
    const survey = templatePreviewSurvey(template({}), "link");
    expect(survey.name).toBe("Product feedback");
    expect(survey.welcomeCard.enabled).toBe(false);
    expect(survey.blocks).toEqual([]);
    expect(survey.endings).toEqual([]);
    expect(survey.hiddenFields).toEqual({ enabled: true, fieldIds: [] });
    expect(survey.variables).toEqual([]);
    expect(survey.metadata).toEqual({});
  });
});
