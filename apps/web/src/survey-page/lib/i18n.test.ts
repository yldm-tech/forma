import { describe, expect, test } from "vite-plus/test";
import enUS from "../../../locales/en-US.json";
import { AVAILABLE_LOCALES } from "../../lib/i18n";
import { SURVEY_PAGE_NAMESPACES, initSurveyPageI18n, surveyPageCatalogues } from "./i18n";

describe("survey page catalogues", () => {
  test("every shipped locale has one, carrying only the public namespaces", async () => {
    expect(Object.keys(surveyPageCatalogues).sort()).toEqual([...AVAILABLE_LOCALES].sort());
    for (const locale of AVAILABLE_LOCALES) {
      const catalogue = await surveyPageCatalogues[locale]();
      expect(Object.keys(catalogue).sort()).toEqual([...SURVEY_PAGE_NAMESPACES].sort());
    }
    expect((await surveyPageCatalogues["en-US"]()).s).toEqual(enUS.s);
  });

  test("translates in the requested locale, a locale the app does not ship in the closest one it does", async () => {
    const ja = await initSurveyPageI18n("ja-JP");
    expect(ja.t("s.paused_heading")).not.toBe(enUS.s.paused_heading);
    const de = await initSurveyPageI18n("de-DE");
    expect(de.t("s.paused_heading")).toBe(enUS.s.paused_heading);
    await ja.changeLanguage("zh-TW");
    expect(ja.t("s.paused_heading")).not.toBe(enUS.s.paused_heading);
  });
});
