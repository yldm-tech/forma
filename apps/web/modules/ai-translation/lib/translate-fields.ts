import "server-only";
import { z } from "zod";
import { logger } from "@forma/logger";
import { generateOrganizationAIObject } from "@/lib/ai/service";
import { AI_TRACING_FEATURE } from "@/lib/posthog/ai-tracing-feature";

export const ZAITranslationField = z.object({
  path: z.string(),
  defaultText: z.string(),
  isRichText: z.boolean(),
});

export type TAITranslationField = z.infer<typeof ZAITranslationField>;

const AI_TRANSLATION_TIMEOUT_MS = 45_000;
const AI_TRANSLATION_MIN_OUTPUT_TOKENS = 1024;
const AI_TRANSLATION_MAX_OUTPUT_TOKENS = 8192;
const AI_TRANSLATION_OUTPUT_TOKENS_PER_FIELD = 160;

/**
 * How many fields one model call may carry. Derived rather than picked: above this the budget the
 * file already declares per field no longer fits in the per-call output cap, so the model is asked
 * for a response it is not allowed to finish and the reply stops mid-JSON. A survey's field count is
 * bounded by nothing, so without this the request size grows with the survey until it stops working.
 */
const AI_TRANSLATION_MAX_FIELDS_PER_CHUNK = Math.floor(
  AI_TRANSLATION_MAX_OUTPUT_TOKENS / AI_TRANSLATION_OUTPUT_TOKENS_PER_FIELD
);

/**
 * What a translation run produced. Partial by design: the fields a chunk returned are kept, and the
 * paths that did not come back are named so the caller can retry or report just those. The previous
 * all-or-nothing contract discarded every translation in the batch over one missing key, which costs
 * the whole run and every token it was billed for.
 */
export interface TAITranslationResult {
  translations: Record<string, string>;
  /** Requested paths with no usable translation. Empty on a fully successful run. */
  failedPaths: string[];
}

interface TranslateFieldsInput {
  organizationId: string;
  workspaceId: string;
  // Attribution only. An API-key caller has no user, so the call runs untraced.
  userId?: string | null;
  fields: TAITranslationField[];
  sourceLanguage: string;
  targetLanguage: string;
}

export const translateFields = async ({
  organizationId,
  workspaceId,
  userId,
  fields,
  sourceLanguage,
  targetLanguage,
}: TranslateFieldsInput): Promise<TAITranslationResult> => {
  if (fields.length === 0) {
    return { translations: {}, failedPaths: [] };
  }

  // Empty defaultText is valid per the schema but has no meaningful translation.
  // Echo it through unchanged so callers still see every requested path in the
  // result, instead of aborting the whole batch when the model "fails" to
  // translate an empty string.
  const translatableFields: TAITranslationField[] = [];
  const passthroughTranslations: Record<string, string> = {};
  for (const field of fields) {
    if (field.defaultText.length === 0) {
      passthroughTranslations[field.path] = "";
    } else {
      translatableFields.push(field);
    }
  }

  if (translatableFields.length === 0) {
    return { translations: passthroughTranslations, failedPaths: [] };
  }

  const systemPrompt = `You are a professional translator for survey content. Translate each item from ${sourceLanguage} to ${targetLanguage}.

Rules:
- For rich text items (richText: true), preserve all HTML tags exactly. Only translate the text content within the tags.
- Preserve any {{variable}} patterns exactly — do not translate text inside double curly braces.
- Translate every item. Do not omit any keys.`;

  const chunks: TAITranslationField[][] = [];
  for (let offset = 0; offset < translatableFields.length; offset += AI_TRANSLATION_MAX_FIELDS_PER_CHUNK) {
    chunks.push(translatableFields.slice(offset, offset + AI_TRANSLATION_MAX_FIELDS_PER_CHUNK));
  }

  const translations: Record<string, string> = {};
  const failedPaths: string[] = [];
  // Kept so a run where nothing at all came back can rethrow the provider's own error — the code on
  // it (quota, timeout) is what the caller maps to a message, and an empty success would lose it.
  let firstChunkError: unknown = null;

  // Sequential rather than parallel: the chunks share one organization's provider quota, and firing
  // them at once is what trips its rate limit.
  for (const chunk of chunks) {
    // Indexed IDs insulate the LLM from user-supplied paths (dots, casing,
    // separator normalization). We map back to paths after generation.
    const items = chunk.map((f, i) => ({
      id: `t${i}`,
      path: f.path,
      text: f.defaultText,
      richText: f.isRichText,
    }));

    // Schema with explicit keys forces the provider to return exactly this set.
    const schema = z.object(Object.fromEntries(items.map((item) => [item.id, z.string()])));

    const userPayload = JSON.stringify(items.map(({ id, text, richText }) => ({ id, text, richText })));

    let translatedById: Record<string, unknown>;
    try {
      const result = await generateOrganizationAIObject({
        organizationId,
        aiTracing: userId
          ? { distinctId: userId, feature: AI_TRACING_FEATURE.Translation, workspaceId }
          : undefined,
        schema,
        system: systemPrompt,
        prompt: userPayload,
        temperature: 0,
        maxOutputTokens: Math.min(
          AI_TRANSLATION_MAX_OUTPUT_TOKENS,
          Math.max(AI_TRANSLATION_MIN_OUTPUT_TOKENS, chunk.length * AI_TRANSLATION_OUTPUT_TOKENS_PER_FIELD)
        ),
        timeout: AI_TRANSLATION_TIMEOUT_MS,
      });
      translatedById = result.object;
    } catch (error) {
      firstChunkError ??= error;
      for (const item of items) failedPaths.push(item.path);
      logger.error(
        { error, organizationId, sourceLanguage, targetLanguage, chunkSize: chunk.length },
        "AI translation chunk failed"
      );
      continue;
    }

    const missingIds: string[] = [];
    for (const item of items) {
      const value = translatedById[item.id];
      if (typeof value === "string" && value.length > 0) {
        translations[item.path] = value;
      } else {
        missingIds.push(item.id);
        failedPaths.push(item.path);
      }
    }

    if (missingIds.length > 0) {
      logger.error(
        {
          organizationId,
          sourceLanguage,
          targetLanguage,
          requestedCount: chunk.length,
          returnedCount: chunk.length - missingIds.length,
          missingIds,
        },
        "AI translation returned incomplete result"
      );
    }
  }

  // Nothing usable came back from any chunk. That is a failed run, not a partial one, and the caller
  // has to be able to tell the difference — a partial result it can keep, an empty one it cannot.
  if (Object.keys(translations).length === 0) {
    if (firstChunkError) throw firstChunkError;
    throw new Error("AI translation returned incomplete result");
  }

  return { translations: { ...passthroughTranslations, ...translations }, failedPaths };
};
