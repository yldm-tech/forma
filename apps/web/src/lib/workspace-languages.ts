import { LANGUAGE_CANONICAL_MAP, normalizeLanguageCode } from "@forma/i18n-utils/canonical";
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
 * - aliases_exchanged: the rows are valid together, but the server checks each written row against the rows stored when it arrives, and no order of the writes passes: two rows exchange aliases (or more pass them round), so each waits for the other to give its alias up.
 */
export type LanguagesProblem =
  | { kind: "missing_language" }
  | { kind: "language_twice"; language: string }
  | { kind: "alias_twice"; alias: string; language: string; other: string }
  | { kind: "alias_names_other_row"; alias: string; language: string; other: string }
  | { kind: "alias_names_missing_language"; alias: string; language: string; named: string }
  | { kind: "aliases_exchanged"; language: string; other: string };

const trimmedAlias = (row: LanguageRowValue) => (row.alias ?? "").trim();

const canonicalByLowercase = new Map(
  Object.entries(LANGUAGE_CANONICAL_MAP).map(([code, canonical]) => [code.toLowerCase(), canonical])
);

/**
 * The canonical tag the alias rule reads a code or alias as, or null when it names no language. This is the server's langcode.Normalize (apps/server/internal/langcode/langcode.go), not normalizeLanguageCode: the static table, else a well-formed tag that names its region itself (Chinese also its script), with no CLDR fallback, so `jpn`, `eng` or `zh-SG` name no language here as they name none there, and the form refuses exactly what the server refuses.
 */
export const aliasRuleLanguageCode = (value: string): string | null => {
  const cleaned = value.trim().replace(/_/g, "-");
  if (!cleaned) return null;
  const known = LANGUAGE_CANONICAL_MAP[cleaned] ?? canonicalByLowercase.get(cleaned.toLowerCase());
  if (known) return known;
  const subtags = cleaned.split("-");
  if (subtags.some((subtag) => !/^[A-Za-z0-9]{1,8}$/.test(subtag))) return null;
  const [first = "", ...rest] = subtags;
  if (!/^[A-Za-z]+$/.test(first) || first.length < 2 || first.length === 4) return null;
  const language = first.toLowerCase();
  let script = "";
  if (rest[0] !== undefined && /^[A-Za-z]{4}$/.test(rest[0])) {
    script = rest[0].charAt(0).toUpperCase() + rest[0].slice(1).toLowerCase();
    rest.shift();
  }
  const region =
    rest[0] !== undefined && /^(?:[A-Za-z]{2}|[0-9]{3})$/.test(rest[0]) ? rest[0].toUpperCase() : "";
  if (!region || (language === "zh" && !script)) return null;
  return [language, script, region].filter(Boolean).join("-");
};

/** A code or alias as the rule compares it: its canonical tag when it names a language, else itself, lowercased either way. */
const comparable = (value: string) => {
  const trimmed = value.trim();
  return (aliasRuleLanguageCode(trimmed) ?? trimmed).toLowerCase();
};

/** The base language (primary subtag) a code or alias names, or null when it names no language. */
const baseLanguage = (value: string): string | null => {
  const canonical = aliasRuleLanguageCode(value);
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
 * The first problem that keeps the rows from being saved, or null. Only what the edit touches is checked: a row added or whose alias changed, and the rows it clashes with, so a stored row an older rule let in never blocks saving the others. Languages compare by canonical code and an alias by the base language it names: an alias may name its own row's language (ja for ja-JP, zh for zh-Hans-CN) or no language at all, but not another row's language or one the workspace does not have. The server checks each row it creates or changes the same way (apps/server/internal/workspace/language.go), one request at a time, so the rows must also be writable in some order (planWrites).
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
        named: aliasRuleLanguageCode(alias) ?? alias,
      };
    }
  }
  const plan = planWrites(languages, stored);
  return "stuck" in plan ? plan.stuck : null;
};

/** A request the form sends to save: a row added in this edit, a stored row whose alias changed, or a removed row. */
export type LanguageWrite =
  | { kind: "create"; row: LanguageRowValue }
  | { kind: "update"; row: LanguageRowValue }
  | { kind: "delete"; id: string };

/** Whether the server refuses the write of row while the stored rows are current: CreateLanguage's newLanguageProblem for a new row, UpdateLanguage's aliasProblem for a changed alias (apps/server/internal/workspace/language.go). */
const serverRefuses = (row: LanguageRowValue, current: LanguageRowValue[]): boolean => {
  const others = current.filter((other) => other.id !== row.id);
  if (row.id === NEW_LANGUAGE_ID) {
    if (others.some((other) => comparable(other.code) === comparable(row.code))) return true;
    // A stored alias that names the new language.
    if (others.some((other) => aliasConflict(other, row) !== null)) return true;
  } else {
    const before = current.find((other) => other.id === row.id);
    // An alias written back unchanged is not checked.
    if (before && trimmedAlias(before) === trimmedAlias(row)) return false;
  }
  if (others.some((other) => aliasConflict(row, other) !== null)) return true;
  const alias = trimmedAlias(row);
  const base = alias ? baseLanguage(alias) : null;
  return (
    base !== null &&
    base !== baseLanguage(row.code) &&
    !others.some((other) => baseLanguage(other.code) === base)
  );
};

/**
 * The writes that save the rows, in an order the server accepts, or why there is none. The server checks each write on its own against the rows stored when it arrives, so a row taking an alias another row gives up must be written after that row. Writes the server accepts as things stand go first, in the form's order; a removal is sent only once no write can go, since it may be what a write waits for (an alias moved off a removed row). A write that no order lets through is an exchange of aliases.
 */
const planWrites = (
  languages: LanguageRowValue[],
  stored: LanguageRowValue[]
): { writes: LanguageWrite[] } | { stuck: LanguagesProblem } => {
  let pending = languages.filter((row) => {
    if (row.id === NEW_LANGUAGE_ID) return true;
    const before = stored.find((candidate) => candidate.id === row.id);
    return !before || (before.alias || "") !== (row.alias || "");
  });
  const removals = stored.filter((row) => !languages.some((kept) => kept.id === row.id)).map((row) => row.id);
  let current = [...stored];
  const writes: LanguageWrite[] = [];
  while (pending.length > 0) {
    const next = pending.find((row) => !serverRefuses(row, current));
    if (next) {
      writes.push({ kind: next.id === NEW_LANGUAGE_ID ? "create" : "update", row: next });
      pending = pending.filter((row) => row !== next);
      current =
        next.id === NEW_LANGUAGE_ID
          ? [...current, next]
          : current.map((row) => (row.id === next.id ? next : row));
      continue;
    }
    const id = removals.shift();
    if (id === undefined) {
      const row = pending[0]!;
      const other = current.find(
        (candidate) =>
          candidate.id !== row.id &&
          (aliasConflict(row, candidate) !== null || aliasConflict(candidate, row) !== null)
      );
      return { stuck: { kind: "aliases_exchanged", language: row.code, other: other?.code ?? row.code } };
    }
    writes.push({ kind: "delete", id });
    current = current.filter((row) => row.id !== id);
  }
  return { writes: [...writes, ...removals.map((id): LanguageWrite => ({ kind: "delete", id }))] };
};

/** The requests that save the rows, one at a time in this order, for rows languagesError accepts. */
export const languageWrites = (
  languages: LanguageRowValue[],
  stored: LanguageRowValue[]
): LanguageWrite[] => {
  const plan = planWrites(languages, stored);
  if ("stuck" in plan) throw new Error(`languages cannot be saved: ${plan.stuck.kind}`);
  return plan.writes;
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
