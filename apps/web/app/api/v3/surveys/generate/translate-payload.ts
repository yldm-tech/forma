import { logger } from "@forma/logger";
import { translateFields } from "@/modules/ai-translation/lib/translate-fields";
import type { TV3CreateSurveyBody } from "../schemas";

/**
 * Fills a generated survey's extra languages.
 *
 * Kept out of `buildV3SurveyCreatePayloadFromDraft` on purpose: that function is documented as pure
 * and synchronous, and both the blocking and the streaming route converge on it. Translation is
 * network I/O and belongs after it.
 *
 * Attaching a language without its text is not an option — `prepareV3SurveyCreateInput` rejects a
 * payload whose translatable fields are missing a configured language, so a survey either carries
 * every language in full or does not claim the language at all.
 */

/** What `text()` produces in the builder: one key, the language the model wrote in. */
const isSingleLanguageText = (value: unknown, sourceLanguage: string): value is Record<string, string> => {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const entries = Object.entries(value as Record<string, unknown>);
  return entries.length === 1 && entries[0][0] === sourceLanguage && typeof entries[0][1] === "string";
};

/**
 * Every translatable value in the payload, by the path the walk reached it on. The path is opaque —
 * it only has to round-trip through `translateFields`, which echoes back the keys it was given.
 */
const collectTranslatableFields = (
  node: unknown,
  sourceLanguage: string,
  path: string,
  found: Map<string, Record<string, string>>
): void => {
  if (!node || typeof node !== "object") return;

  if (isSingleLanguageText(node, sourceLanguage)) {
    found.set(path, node as Record<string, string>);
    return;
  }

  if (Array.isArray(node)) {
    node.forEach((item, index) => collectTranslatableFields(item, sourceLanguage, `${path}.${index}`, found));
    return;
  }

  for (const [key, value] of Object.entries(node as Record<string, unknown>)) {
    collectTranslatableFields(value, sourceLanguage, path ? `${path}.${key}` : key, found);
  }
};

/**
 * Where a translation run has got to. `index` is 1-based, so it reads as "index of total" wherever a
 * caller puts it in front of a user.
 */
export interface TV3TranslationProgress {
  languageCode: string;
  index: number;
  total: number;
}

export const translateV3SurveyPayloadLanguages = async ({
  payload,
  sourceLanguage,
  targetLanguages,
  organizationId,
  workspaceId,
  userId,
  onLanguageStart,
}: {
  payload: TV3CreateSurveyBody;
  sourceLanguage: string;
  targetLanguages: string[];
  organizationId: string;
  workspaceId: string;
  // Attribution only. An API-key caller has no user, so the translation runs untraced.
  userId?: string | null;
  /**
   * Called before each language's model call. This phase runs after the last generated token and
   * takes tens of seconds per language, so a streaming caller has nothing to send in the meantime —
   * and a body that goes quiet for minutes is indistinguishable from a dead socket to any proxy with
   * an idle read timeout.
   */
  onLanguageStart?: (progress: TV3TranslationProgress) => void;
}): Promise<TV3CreateSurveyBody> => {
  const wanted = targetLanguages.filter((code) => code.toLowerCase() !== sourceLanguage.toLowerCase());
  if (wanted.length === 0) return payload;

  // Mutated in place, so every reference the walk collected points at the object being filled.
  const translated = structuredClone(payload) as TV3CreateSurveyBody;
  const fieldsByPath = new Map<string, Record<string, string>>();
  collectTranslatableFields(translated, sourceLanguage, "", fieldsByPath);

  const fields = Array.from(fieldsByPath, ([path, value]) => ({
    path,
    defaultText: value[sourceLanguage] ?? "",
    // A generated draft has no rich text: the model returns plain strings, and the builder stores
    // them unchanged. Marking them rich would have the translator preserve markup that is not there.
    isRichText: false,
  }));

  // Sequential rather than parallel: each language is a separate model call against the same
  // organization's quota, and a burst of them is what trips the provider's rate limit.
  const added: string[] = [];
  for (const [position, targetLanguage] of wanted.entries()) {
    onLanguageStart?.({ languageCode: targetLanguage, index: position + 1, total: wanted.length });

    try {
      const { translations, failedPaths } = await translateFields({
        organizationId,
        workspaceId,
        userId,
        fields,
        sourceLanguage,
        targetLanguage,
      });

      // A partial result cannot be attached here even though it is worth keeping elsewhere:
      // `prepareV3SurveyCreateInput` rejects a payload whose translatable fields are missing a
      // configured language, so a language with a gap in it has to be dropped like a failed one.
      if (failedPaths.length > 0) {
        logger.error(
          { targetLanguage, sourceLanguage, workspaceId, failedCount: failedPaths.length },
          "Dropped a generated survey language that was only partially translated"
        );
        continue;
      }

      for (const [path, value] of fieldsByPath) {
        const text = translations[path];
        if (typeof text === "string") value[targetLanguage] = text;
      }
      added.push(targetLanguage);
    } catch (error) {
      // One language failing does not cost the draft. The survey keeps the languages that did
      // translate, and the author can add the rest from the editor's translation flow — which is a
      // better outcome than discarding a generation that already succeeded.
      logger.error(
        { error, targetLanguage, sourceLanguage, workspaceId },
        "Failed to translate a generated survey into a requested language"
      );
    }
  }

  translated.languages = [
    { code: sourceLanguage, default: true, enabled: true },
    ...added.map((code) => ({ code, default: false, enabled: true })),
  ];

  return translated;
};
