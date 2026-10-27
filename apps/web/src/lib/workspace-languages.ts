import { normalizeLanguageCode } from "@forma/i18n-utils/canonical";
import { isSurveyRuntimeLanguage } from "@forma/i18n-utils/survey-runtime-languages";
import { getLanguageLabel } from "@forma/i18n-utils/utils";

// forma2's rules for the workspace languages form (modules/survey/multi-language-surveys/components/edit-language.tsx).

/** A row of the form: a stored language, or "new" for one added in this edit. */
export interface LanguageRowValue {
  id: string;
  code: string;
  alias: string | null;
}

export const NEW_LANGUAGE_ID = "new";

/**
 * Why the rows cannot be saved, naming the rows involved by their language codes (the page shows language names).
 * - missing_language: a new row has no language picked.
 * - language_twice: two rows are the same language.
 * - alias_twice: two rows share an alias.
 * - alias_names_other_row: the alias is another row's code or names another row's language, so `?lang=<alias>` would not open the language its own row is.
 * - alias_names_missing_language: the alias is the code of a language the workspace does not have, so the alias and that code would mean different languages.
 */
export type LanguagesProblem =
  | { kind: "missing_language" }
  | { kind: "language_twice"; language: string }
  | { kind: "alias_twice"; alias: string; language: string; other: string }
  | { kind: "alias_names_other_row"; alias: string; language: string; other: string }
  | { kind: "alias_names_missing_language"; alias: string; language: string; named: string };

const trimmedAlias = (row: LanguageRowValue) => (row.alias ?? "").trim();

/** A code or alias as the rule compares it: its canonical tag when it names a language, else itself, lowercased either way. */
const comparable = (value: string) => {
  const trimmed = value.trim();
  return (normalizeLanguageCode(trimmed) ?? trimmed).toLowerCase();
};

/** The base language (primary subtag) a code or alias names, or null when it names no language. */
const baseLanguage = (value: string): string | null => {
  const canonical = normalizeLanguageCode(value.trim());
  return canonical ? (canonical.split("-")[0] ?? "").toLowerCase() : null;
};

/** Whether the row was added in this edit or its alias changed. */
const isChanged = (row: LanguageRowValue, stored: LanguageRowValue[]) => {
  const before = stored.find((candidate) => candidate.id === row.id);
  return row.id === NEW_LANGUAGE_ID || !before || trimmedAlias(before) !== trimmedAlias(row);
};

/** What is wrong between the alias of row and another row of the form, if anything. */
const aliasConflict = (row: LanguageRowValue, other: LanguageRowValue): LanguagesProblem | null => {
  const alias = trimmedAlias(row);
  if (!alias || !other.code.trim()) return null;
  const lowerAlias = alias.toLowerCase();
  if (trimmedAlias(other).toLowerCase() === lowerAlias) {
    return { kind: "alias_twice", alias, language: row.code, other: other.code };
  }
  const base = baseLanguage(alias);
  if (
    other.code.trim().toLowerCase() === lowerAlias ||
    comparable(other.code) === comparable(alias) ||
    (base !== null && base !== baseLanguage(row.code) && base === baseLanguage(other.code))
  ) {
    return { kind: "alias_names_other_row", alias, language: row.code, other: other.code };
  }
  return null;
};

/**
 * The first problem that keeps the rows from being saved, or null. Only what the edit touches is checked: a row added or whose alias changed, and the rows it clashes with, so a stored row an older rule let in never blocks saving the others. Languages compare by canonical code and an alias by the base language it names: an alias may name its own row's language (ja for ja-JP, zh for zh-Hans-CN) or no language at all, but not another row's language or one the workspace does not have. The server checks each row it creates or changes the same way (apps/server/internal/workspace/language.go).
 */
export const languagesError = (
  languages: LanguageRowValue[],
  stored: LanguageRowValue[]
): LanguagesProblem | null => {
  const changed = languages.map((row) => isChanged(row, stored));
  if (languages.some((row, index) => changed[index] && !row.code.trim())) return { kind: "missing_language" };
  // The rows the edit changed come first, so a clash is reported on the row the user touched.
  const order = languages.map((_, index) => index).sort((a, b) => Number(changed[b]) - Number(changed[a]));
  for (const index of order) {
    const row = languages[index]!;
    for (const [otherIndex, other] of languages.entries()) {
      if (otherIndex === index || !(changed[index] || changed[otherIndex])) continue;
      if (row.code.trim() && other.code.trim() && comparable(row.code) === comparable(other.code)) {
        return { kind: "language_twice", language: changed[index] ? row.code : other.code };
      }
      const conflict = aliasConflict(row, other);
      if (conflict) return conflict;
    }
    const alias = trimmedAlias(row);
    const base = alias ? baseLanguage(alias) : null;
    if (
      changed[index] &&
      base !== null &&
      base !== baseLanguage(row.code) &&
      !languages.some((other) => baseLanguage(other.code) === base)
    ) {
      return {
        kind: "alias_names_missing_language",
        alias,
        language: row.code,
        named: normalizeLanguageCode(alias) ?? alias,
      };
    }
  }
  return null;
};

/** forma2's isWorkspaceDefaultSurveyLanguage: a row is the default when both name the same canonical language. */
export const isDefaultSurveyLanguage = (
  languageCode: string,
  defaultLanguage: string | null | undefined
): boolean => {
  const canonicalDefault = normalizeLanguageCode(defaultLanguage ?? "");
  return canonicalDefault !== null && normalizeLanguageCode(languageCode) === canonicalDefault;
};

export interface DefaultLanguageOption {
  code: string;
  label: string;
  isSelectable: boolean;
}

/** The default-language choices: one per canonical language of the form, the ones the survey runtime has strings for first, then by label. */
export const defaultLanguageOptions = (
  languages: LanguageRowValue[],
  locale: string
): DefaultLanguageOption[] => {
  const byCode = new Map<string, DefaultLanguageOption>();
  for (const language of languages) {
    const code = normalizeLanguageCode(language.code) ?? language.code;
    if (!code || byCode.has(code)) continue;
    byCode.set(code, {
      code,
      label: getLanguageLabel(code, locale) ?? code,
      isSelectable: isSurveyRuntimeLanguage(code),
    });
  }
  return [...byCode.values()].sort(
    (left, right) =>
      Number(right.isSelectable) - Number(left.isSelectable) || left.label.localeCompare(right.label, locale)
  );
};
