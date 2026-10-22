import { normalizeLanguageCode } from "@forma/i18n-utils/canonical";
import { isSurveyRuntimeLanguage } from "@forma/i18n-utils/survey-runtime-languages";
import { getLanguageLabel, iso639Languages } from "@forma/i18n-utils/utils";

// forma2's rules for the workspace languages form (modules/survey/multi-language-surveys/components/edit-language.tsx).

/** A row of the form: a stored language, or "new" for one added in this edit. */
export interface LanguageRowValue {
  id: string;
  code: string;
  alias: string | null;
}

export const NEW_LANGUAGE_ID = "new";

const hasDuplicates = (values: string[]) => new Set(values).size !== values.length;

/**
 * forma2's validateLanguages: the translation key of the first problem, or null when the rows can be saved. Codes and aliases compare lowercased and trimmed: no blank code, no code or alias twice, no alias equal to a code of the form, and no alias that is the ISO code of a language the form does not have.
 */
export const languagesError = (languages: LanguageRowValue[]): string | null => {
  const codes = languages.map((language) => language.code.toLowerCase().trim());
  const aliases = languages
    .filter((language) => language.alias)
    .map((language) => language.alias!.toLowerCase().trim());
  if (codes.includes("")) return "workspace.languages.please_select_a_language";
  if (hasDuplicates(aliases) || hasDuplicates(codes))
    return "workspace.languages.duplicate_language_or_language_id";
  if (codes.some((code) => aliases.includes(code)))
    return "workspace.languages.conflict_between_identifier_and_alias";
  for (const alias of aliases) {
    if (iso639Languages.some((language) => language.code === alias && !codes.includes(alias))) {
      return "workspace.languages.conflict_between_selected_alias_and_another_language";
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
