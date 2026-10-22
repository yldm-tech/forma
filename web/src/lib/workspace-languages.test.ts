import { expect, test } from "vite-plus/test";
import { defaultLanguageOptions, isDefaultSurveyLanguage, languagesError } from "./workspace-languages";

const row = (code: string, alias: string | null = null, id = code) => ({ id, code, alias });

test("every row needs a language", () => {
  expect(languagesError([row("en-US"), row("", null, "new")])).toBe(
    "workspace.languages.please_select_a_language"
  );
});

test("a code or alias may appear once", () => {
  expect(languagesError([row("en-US"), row("EN-US ", null, "new")])).toBe(
    "workspace.languages.duplicate_language_or_language_id"
  );
  expect(languagesError([row("en-US", "x"), row("de-DE", "X")])).toBe(
    "workspace.languages.duplicate_language_or_language_id"
  );
});

test("an alias may not name a code of the form", () => {
  expect(languagesError([row("en-US"), row("de-DE", "en-us")])).toBe(
    "workspace.languages.conflict_between_identifier_and_alias"
  );
});

test("an alias may not be the ISO code of another language", () => {
  expect(languagesError([row("en-US", "nl")])).toBe(
    "workspace.languages.conflict_between_selected_alias_and_another_language"
  );
  expect(languagesError([row("en-US", "english")])).toBeNull();
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
