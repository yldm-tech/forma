import { expect, test } from "vite-plus/test";
import { defaultLanguageOptions, isDefaultSurveyLanguage, languagesError } from "./workspace-languages";

const row = (code: string, alias: string | null = null, id = code) => ({ id, code, alias });

const NEW = "new";

test("a new row needs a language", () => {
  expect(languagesError([row("en-US"), row("", null, NEW)], [row("en-US")])).toEqual({
    kind: "missing_language",
  });
});

test("a language may be in the form once, compared canonically", () => {
  expect(languagesError([row("de-DE"), row("de", null, NEW)], [row("de-DE")])).toEqual({
    kind: "language_twice",
    language: "de",
  });
});

test("an alias may name its own row's language", () => {
  // The base-language aliases forma2 refused although they mean the row they are on.
  expect(languagesError([row("ja-JP", "ja", NEW)], [])).toBeNull();
  expect(languagesError([row("zh-Hans-CN", "zh", NEW)], [])).toBeNull();
  expect(languagesError([row("zh-Hant-TW", "zh-TW", NEW)], [])).toBeNull();
  expect(languagesError([row("en-US", "en_us", NEW)], [])).toBeNull();
  expect(languagesError([row("en-US", "english", NEW)], [])).toBeNull();
});

test("an alias may not name a language the workspace does not have", () => {
  expect(languagesError([row("ja-JP", "fr", NEW)], [])).toEqual({
    kind: "alias_names_missing_language",
    alias: "fr",
    language: "ja-JP",
    named: "fr-FR",
  });
  expect(languagesError([row("en-US", "nl")], [row("en-US")])).toEqual({
    kind: "alias_names_missing_language",
    alias: "nl",
    language: "en-US",
    named: "nl-NL",
  });
});

test("an alias may not name another row", () => {
  expect(languagesError([row("en-US"), row("de-DE", "en-us")], [row("en-US"), row("de-DE")])).toEqual({
    kind: "alias_names_other_row",
    alias: "en-us",
    language: "de-DE",
    other: "en-US",
  });
  // ?lang=ja would open German.
  expect(languagesError([row("ja-JP"), row("de-DE", "ja")], [row("ja-JP"), row("de-DE")])).toEqual({
    kind: "alias_names_other_row",
    alias: "ja",
    language: "de-DE",
    other: "ja-JP",
  });
  // zh is zh-Hans-CN, a row of its own, even on a Traditional Chinese row.
  expect(languagesError([row("zh-Hans-CN"), row("zh-Hant-TW", "zh", NEW)], [row("zh-Hans-CN")])).toEqual({
    kind: "alias_names_other_row",
    alias: "zh",
    language: "zh-Hant-TW",
    other: "zh-Hans-CN",
  });
});

test("an alias may be used once", () => {
  expect(languagesError([row("en-US", "x"), row("de-DE", " X ", NEW)], [row("en-US", "x")])).toEqual({
    kind: "alias_twice",
    alias: "X",
    language: "de-DE",
    other: "en-US",
  });
});

test("only what the edit touches is checked", () => {
  // Seed's stored aliases do not block adding Arabic.
  const seed = [row("en-US"), row("ja-JP", "ja"), row("zh-Hans-CN", "zh")];
  expect(languagesError([...seed, row("ar-EG", null, NEW)], seed)).toBeNull();
  // A stored alias forma2 let in does not block the other rows either.
  const legacy = [row("en-US", "nl"), row("de-DE")];
  expect(languagesError([legacy[0]!, row("de-DE", "german")], legacy)).toBeNull();
  // But a new row that a stored alias names is the edit's clash.
  expect(languagesError([...legacy, row("nl-NL", null, NEW)], legacy)).toEqual({
    kind: "alias_names_other_row",
    alias: "nl",
    language: "en-US",
    other: "nl-NL",
  });
});

test("the default language compares canonical codes", () => {
  expect(isDefaultSurveyLanguage("de", "de-DE")).toBe(true);
  expect(isDefaultSurveyLanguage("es-MX", "es-ES")).toBe(false);
  expect(isDefaultSurveyLanguage("en-US", null)).toBe(false);
});

test("default-language options collapse legacy codes and list runtime languages first", () => {
  const options = defaultLanguageOptions([row("de"), row("de-DE"), row("en-US")], "en-US");
  expect(options.map((option) => option.code).sort()).toEqual(["de-DE", "en-US"]);
  expect(options.every((option) => option.isSelectable)).toBe(true);
});
