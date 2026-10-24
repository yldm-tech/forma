import { describe, expect, test } from "vite-plus/test";
import { getDateFnsLocale } from "./date-fns-locale";

describe("getDateFnsLocale", () => {
  // The calendar reads month, weekday and first-day-of-week off the returned locale, so the assertions are on the resolved locale's `code` rather than on object identity.
  test.each([
    ["de-DE", "de"],
    ["es-ES", "es"],
    ["fr-FR", "fr"],
    ["hu-HU", "hu"],
    ["ja-JP", "ja"],
    ["nl-NL", "nl"],
    ["ro-RO", "ro"],
    ["ru-RU", "ru"],
    ["sv-SE", "sv"],
    ["tr-TR", "tr"],
    ["en-US", "en-US"],
  ])("maps the app locale %s to date-fns %s", (appLocale, expected) => {
    expect(getDateFnsLocale(appLocale).code).toBe(expected);
  });

  test.each([
    ["pt-BR", "pt-BR"],
    ["pt-PT", "pt"],
    ["pt", "pt-BR"],
  ])("keeps Portuguese variants apart: %s", (appLocale, expected) => {
    // pt-BR and pt-PT are different locales, so neither may be reached by cutting the tag to "pt".
    expect(getDateFnsLocale(appLocale).code).toBe(expected);
  });

  test.each([
    ["zh-Hans-CN", "zh-CN"],
    ["zh-cn", "zh-CN"],
    ["zh-Hant-TW", "zh-TW"],
    ["zh-tw", "zh-TW"],
    ["zh-hk", "zh-TW"],
    ["zh", "zh-CN"],
  ])("resolves Chinese script tags: %s", (appLocale, expected) => {
    expect(getDateFnsLocale(appLocale).code).toBe(expected);
  });

  test("is case-insensitive about the tag", () => {
    expect(getDateFnsLocale("DE-de").code).toBe("de");
    expect(getDateFnsLocale("PT-br").code).toBe("pt-BR");
  });

  test("falls back to en-US for an unset, empty or unknown locale", () => {
    // A survey language that never became an app locale must not throw.
    expect(getDateFnsLocale().code).toBe("en-US");
    expect(getDateFnsLocale("").code).toBe("en-US");
    expect(getDateFnsLocale("xx-YY").code).toBe("en-US");
    expect(getDateFnsLocale("uz").code).toBe("en-US");
  });

  test("accepts a bare language tag without a region", () => {
    expect(getDateFnsLocale("de").code).toBe("de");
    expect(getDateFnsLocale("ja").code).toBe("ja");
  });
});
