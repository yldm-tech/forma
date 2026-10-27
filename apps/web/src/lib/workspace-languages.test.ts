import { expect, test } from "vite-plus/test";
import aliasCases from "./workspace-language-alias-cases.json";
import {
  aliasRuleLanguageCode,
  defaultLanguageOptions,
  isDefaultSurveyLanguage,
  languageWrites,
  languagesError,
} from "./workspace-languages";

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

test("the alias rule is the server's, case by case", () => {
  // The same cases run in apps/server/internal/workspace/language_test.go.
  expect(aliasCases.cases.length).toBeGreaterThan(0);
  for (const testCase of aliasCases.cases) {
    const existing = testCase.existing.map((stored, index) => row(stored.code, stored.alias, `s${index}`));
    const problem = languagesError([...existing, row(testCase.code, testCase.alias, NEW)], existing);
    expect({ name: testCase.name, ok: problem === null }).toEqual({ name: testCase.name, ok: testCase.ok });
  }
});

test("a code names a language through the table or its own region, never CLDR", () => {
  expect(aliasRuleLanguageCode("JA")).toBe("ja-JP");
  expect(aliasRuleLanguageCode("zh_TW")).toBe("zh-Hant-TW");
  expect(aliasRuleLanguageCode("fr-lu")).toBe("fr-LU");
  expect(aliasRuleLanguageCode("zh-hant-sg")).toBe("zh-Hant-SG");
  for (const unnamed of ["jpn", "eng", "ara", "sh", "zh-SG", "english", "x", "ja-JP-x-?"]) {
    expect({ unnamed, code: aliasRuleLanguageCode(unnamed) }).toEqual({ unnamed, code: null });
  }
});

test("an alias moved to an earlier row is given up before it is taken", () => {
  const stored = [row("en-US", null, "en"), row("ar-EG", "arab", "ar")];
  const languages = [row("en-US", "arab", "en"), row("ar-EG", null, "ar")];
  expect(languagesError(languages, stored)).toBeNull();
  expect(languageWrites(languages, stored)).toEqual([
    { kind: "update", row: row("ar-EG", null, "ar") },
    { kind: "update", row: row("en-US", "arab", "en") },
  ]);
});

test("a legacy alias is cleared before the language it names is added", () => {
  const stored = [row("en-US", "nl", "en")];
  const languages = [row("en-US", null, "en"), row("nl-NL", null, NEW)];
  expect(languagesError(languages, stored)).toBeNull();
  expect(languageWrites(languages, stored).map((write) => write.kind)).toEqual(["update", "create"]);
});

test("a removal goes first only when a write waits for it", () => {
  const stored = [row("en-US", null, "en"), row("de-DE", "main", "de"), row("fr-FR", null, "fr")];
  // The alias of the removed German row moves to English.
  expect(languageWrites([row("en-US", "main", "en"), row("fr-FR", null, "fr")], stored)).toEqual([
    { kind: "delete", id: "de" },
    { kind: "update", row: row("en-US", "main", "en") },
  ]);
  // Otherwise the writes go first and removals last.
  expect(languageWrites([row("en-US", "english", "en"), row("de-DE", "main", "de")], stored)).toEqual([
    { kind: "update", row: row("en-US", "english", "en") },
    { kind: "delete", id: "fr" },
  ]);
});

test("aliases exchanged between rows are refused, since no order of single writes passes", () => {
  const stored = [row("en-US", "a", "en"), row("de-DE", "b", "de")];
  expect(languagesError([row("en-US", "b", "en"), row("de-DE", "a", "de")], stored)).toEqual({
    kind: "aliases_exchanged",
    language: "en-US",
    other: "de-DE",
  });
  // Passed round three rows too.
  const three = [...stored, row("fr-FR", "c", "fr")];
  expect(
    languagesError([row("en-US", "b", "en"), row("de-DE", "c", "de"), row("fr-FR", "a", "fr")], three)
  ).toMatchObject({ kind: "aliases_exchanged" });
});
