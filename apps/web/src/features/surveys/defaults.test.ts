import { describe, expect, test } from "vite-plus/test";
import { resolveDefaultSurveyLanguage, surveyTypeForChannel, workspaceSurveyConfig } from "./defaults";

// forma2's lib/i18n/default-survey-language.test.ts.
describe("resolveDefaultSurveyLanguage", () => {
  const resolve = (workspaceDefaultLanguage: string | null | undefined, userLocale?: string | null) =>
    resolveDefaultSurveyLanguage({ workspaceDefaultLanguage, userLocale });

  test("the workspace setting wins over the creator's locale", () => {
    expect(resolve("de-DE", "en-US")).toBe("de-DE");
  });

  test("a runtime-only language can be the workspace default", () => {
    expect(resolve("it-IT", "en-US")).toBe("it-IT");
  });

  test("a legacy workspace language code resolves to its canonical tag", () => {
    expect(resolve("de", "en-US")).toBe("de-DE");
    expect(resolve("zh-CN", "en-US")).toBe("zh-Hans-CN");
  });

  test("falls back to the creator's locale when the setting is unset", () => {
    expect(resolve(undefined, "fr-FR")).toBe("fr-FR");
    expect(resolve(null, "fr-FR")).toBe("fr-FR");
  });

  test("keeps a regional variant the runtime serves from its language's bundle", () => {
    expect(resolve("es-MX", "en-US")).toBe("es-MX");
    expect(resolve("pt-PT", "en-US")).toBe("pt-PT");
    expect(resolve("de-AT", "en-US")).toBe("de-AT");
  });

  test("falls back to the creator's locale when the setting names a language with no strings", () => {
    expect(resolve("fa-IR", "fr-FR")).toBe("fr-FR");
    expect(resolve("aa-ET", "fr-FR")).toBe("fr-FR");
    expect(resolve("nonsense", "fr-FR")).toBe("fr-FR");
  });

  test("falls back to English when neither the setting nor the locale is usable", () => {
    expect(resolveDefaultSurveyLanguage({})).toBe("en-US");
    expect(resolve(undefined, null)).toBe("en-US");
    // A locale the dashboard does not support is not a valid authoring language either.
    expect(resolve(undefined, "it-IT")).toBe("en-US");
  });
});

describe("surveyTypeForChannel", () => {
  test("website and app workspaces create app surveys, link and unset ones link surveys", () => {
    expect(surveyTypeForChannel("website")).toBe("app");
    expect(surveyTypeForChannel("app")).toBe("app");
    expect(surveyTypeForChannel("link")).toBe("link");
    expect(surveyTypeForChannel(null)).toBe("link");
  });
});

describe("workspaceSurveyConfig", () => {
  test("reads the channel and the default survey language", () => {
    expect(
      workspaceSurveyConfig({
        config: { channel: "website", industry: "saas", defaultSurveyLanguage: "fr-FR" },
      })
    ).toEqual({
      channel: "website",
      defaultSurveyLanguage: "fr-FR",
    });
  });

  test("treats a missing, empty or malformed config as unset", () => {
    const unset = { channel: null, defaultSurveyLanguage: null };
    expect(workspaceSurveyConfig({})).toEqual(unset);
    expect(workspaceSurveyConfig({ config: null })).toEqual(unset);
    expect(workspaceSurveyConfig({ config: { channel: "web", defaultSurveyLanguage: 1 } })).toEqual(unset);
  });
});
