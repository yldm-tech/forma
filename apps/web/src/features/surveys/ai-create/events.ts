import type { InvalidParam } from "../../../lib/api";

/**
 * A snapshot of a draft mid-generation (apps/server/internal/survey/v3generate_stream.go): whole rather than a delta, and unvalidated, so a headline can be half-written and a field can hold a value of the wrong type. Display only; saving uses the done event's payload.
 */
export interface GenerationDraftSnapshot {
  name?: unknown;
  blocks?: unknown;
}

/** The create payload a generation ends with, sent to POST /api/v3/surveys as it is. */
export type GeneratedSurveyPayload = Record<string, unknown> & { blocks?: unknown };

export interface GenerationLanguage {
  code: string;
  default: boolean;
  enabled: boolean;
}

/** forma2's TSurveyGenerationStreamEvent, one per NDJSON line. */
export type SurveyGenerationStreamEvent =
  | { type: "start"; requestId: string }
  | { type: "partial"; seq: number; draft: GenerationDraftSnapshot }
  | { type: "translating"; languageCode: string; index: number; total: number }
  | {
      type: "done";
      language: string;
      payload: GeneratedSurveyPayload;
      validation: { valid: boolean; invalid_params: InvalidParam[]; languages: GenerationLanguage[] };
    }
  | { type: "error"; code: string; detail: string; invalid_params?: InvalidParam[]; retryAfter?: number };

/** forma2's TV3SurveyGenerateBody. The first language is the survey's default and the one the model writes in. */
export interface SurveyGenerateBody {
  workspaceId: string;
  prompt: string;
  type: "link" | "app";
  languages: string[];
}
