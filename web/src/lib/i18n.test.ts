import { describe, expect, test } from "vite-plus/test";
import { changeLocale, initI18n, isLocale, matchLocale } from "./i18n";

describe("matchLocale", () => {
  test.each([
    [["ja-JP"], "ja-JP"],
    [["ja"], "ja-JP"],
    [["zh-TW"], "zh-Hant-TW"],
    [["zh-HK"], "zh-Hant-TW"],
    [["zh-CN"], "zh-Hans-CN"],
    [["zh-Hant"], "zh-Hant-TW"],
    [["zh"], "zh-Hans-CN"],
    [["fr-FR", "ja-JP"], "ja-JP"],
    [["en-GB"], "en-US"],
    [["fr-FR"], "en-US"],
    [[], "en-US"],
    [["*"], "en-US"],
  ])("%j resolves to %s", (requested, expected) => {
    expect(matchLocale(requested)).toBe(expected);
  });
});

test("isLocale", () => {
  expect(isLocale("ja-JP")).toBe(true);
  expect(isLocale("fr-FR")).toBe(false);
});

describe("initI18n", () => {
  test("translates namespaced keys with ICU interpolation", async () => {
    const i18n = await initI18n("en-US");
    expect(i18n.t("common.loading")).toBe("Loading");
    expect(i18n.t("common.error_rate_limit_description")).toBe(
      "Maximum number of requests reached. Please try again later."
    );
  });

  test("loads the requested locale and falls back to en-US", async () => {
    const i18n = await initI18n("ja-JP");
    expect(i18n.language).toBe("ja-JP");
    expect(i18n.t("common.loading")).not.toBe("Loading");
    expect(i18n.t("common.loading")).not.toBe("common.loading");
    expect(i18n.hasResourceBundle("en-US", "common")).toBe(true);
  });

  test("never ships the server-only emails namespace to the browser", async () => {
    const i18n = await initI18n("en-US");
    expect(i18n.hasResourceBundle("en-US", "emails")).toBe(false);
  });

  test("changeLocale loads a catalogue on demand", async () => {
    const i18n = await initI18n("en-US");
    expect(i18n.hasResourceBundle("zh-Hant-TW", "common")).toBe(false);
    await changeLocale(i18n, "zh-Hant-TW");
    expect(i18n.language).toBe("zh-Hant-TW");
    expect(i18n.t("common.loading")).not.toBe("Loading");
  });
});
