import { beforeEach, describe, expect, test, vi } from "vite-plus/test";
import type { TSurveyElement } from "@forma/types/surveys/elements";
import { type TWorkspace } from "@forma/types/workspace";
import * as i18nUtils from "./i18n";
import { replaceElementPresetPlaceholders } from "./templates";

vi.mock("./i18n");

beforeEach(() => {
  vi.clearAllMocks();
  // Mock getLocalizedValue to return the value from the object
  vi.mocked(i18nUtils.getLocalizedValue).mockImplementation(
    (obj: Record<string, string> | undefined, lang: string) => obj?.[lang] || ""
  );
});

describe("Template Utilities", () => {
  describe("replaceElementPresetPlaceholders", () => {
    test("returns original element when workspace is not provided", () => {
      const element = {
        type: "openText",
        headline: { default: "Question about $[workspaceName]?" },
      } as unknown as TSurveyElement;

      const result = replaceElementPresetPlaceholders(element, undefined);

      expect(result).toEqual(element);
    });

    test("replaces workspaceName placeholder in headline", () => {
      const element = {
        type: "openText",
        headline: { default: "How do you like $[workspaceName]?" },
      } as unknown as TSurveyElement;

      const workspace = {
        name: "TestWorkspace",
      } as unknown as TWorkspace;

      const result = replaceElementPresetPlaceholders(element, workspace);

      // The function directly replaces without calling getLocalizedValue in the test scenario
      expect(result.headline?.default).toBe("How do you like TestWorkspace?");
    });

    test("replaces workspaceName placeholder in subheader", () => {
      const element = {
        type: "openText",
        headline: { default: "Question" },
        subheader: { default: "Subheader for $[workspaceName]" },
      } as unknown as TSurveyElement;

      const workspace = {
        name: "TestWorkspace",
      } as unknown as TWorkspace;

      const result = replaceElementPresetPlaceholders(element, workspace);

      expect(result.headline?.default).toBe("Question");
      expect(result.subheader?.default).toBe("Subheader for TestWorkspace");
    });

    test("handles missing headline and subheader", () => {
      const element = {
        type: "openText",
      } as unknown as TSurveyElement;

      const workspace = {
        name: "TestWorkspace",
      } as unknown as TWorkspace;

      const result = replaceElementPresetPlaceholders(element, workspace);

      // A copy, not the element itself.
      expect(result).not.toBe(element);
      expect(result).toEqual(element);
    });
  });
});
