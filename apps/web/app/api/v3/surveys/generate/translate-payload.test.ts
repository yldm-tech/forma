import { readFileSync } from "node:fs";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { translateFields } from "@/modules/ai-translation/lib/translate-fields";
import type { TV3CreateSurveyBody } from "../schemas";
import { translateV3SurveyPayloadLanguages } from "./translate-payload";

vi.mock("@/modules/ai-translation/lib/translate-fields", () => ({
  translateFields: vi.fn(),
}));

vi.mock("@forma/logger", () => ({
  logger: { error: vi.fn(), warn: vi.fn(), info: vi.fn() },
}));

const basePayload = {
  workspaceId: "workspace1",
  type: "link",
  name: "Onboarding",
  status: "draft",
  defaultLanguage: "en-US",
  languages: [{ code: "en-US", default: true, enabled: true }],
  metadata: { title: { "en-US": "Onboarding" } },
  blocks: [
    {
      name: "Experience",
      elements: [{ type: "openText", headline: { "en-US": "What should we improve?" } }],
    },
  ],
} as unknown as TV3CreateSurveyBody;

const call = (targetLanguages: string[], payload = basePayload) =>
  translateV3SurveyPayloadLanguages({
    payload,
    sourceLanguage: "en-US",
    targetLanguages,
    organizationId: "org_1",
    workspaceId: "workspace1",
    userId: "user_1",
  });

beforeEach(() => {
  vi.clearAllMocks();
});

describe("translateV3SurveyPayloadLanguages", () => {
  test("leaves the payload untouched when nothing extra was asked for", async () => {
    const result = await call([]);

    expect(result).toBe(basePayload);
    expect(translateFields).not.toHaveBeenCalled();
  });

  test("ignores a target that is already the source language", async () => {
    const result = await call(["EN-us"]);

    expect(result).toBe(basePayload);
    expect(translateFields).not.toHaveBeenCalled();
  });

  test("fills every translatable field and records the language", async () => {
    vi.mocked(translateFields).mockImplementation(async ({ fields }) => ({
      translations: Object.fromEntries(fields.map((field) => [field.path, `ja:${field.defaultText}`])),
      failedPaths: [],
    }));

    const result = await call(["ja-JP"]);

    // Both the shallow field and the one nested inside an array, since the walk has to reach both.
    expect(result.metadata.title).toEqual({
      "en-US": "Onboarding",
      "ja-JP": "ja:Onboarding",
    });
    expect(
      (result as never as { blocks: { elements: { headline: unknown }[] }[] }).blocks[0].elements[0].headline
    ).toEqual({
      "en-US": "What should we improve?",
      "ja-JP": "ja:What should we improve?",
    });
    expect(result.languages).toEqual([
      { code: "en-US", default: true, enabled: true },
      { code: "ja-JP", default: false, enabled: true },
    ]);
  });

  test("does not mutate the payload it was given", async () => {
    vi.mocked(translateFields).mockResolvedValue({
      translations: { "metadata.title": "translated" },
      failedPaths: [],
    });

    await call(["ja-JP"]);

    expect(basePayload.metadata.title).toEqual({ "en-US": "Onboarding" });
  });

  test("keeps the languages that translated when one of them fails", async () => {
    vi.mocked(translateFields)
      .mockRejectedValueOnce(new Error("provider unavailable"))
      .mockImplementationOnce(async ({ fields }) => ({
        translations: Object.fromEntries(fields.map((field) => [field.path, `de:${field.defaultText}`])),
        failedPaths: [],
      }));

    const result = await call(["ja-JP", "de-DE"]);

    // A generation that already cost a model call is not thrown away because one translation of it
    // failed; the author can add the missing language from the editor.
    expect(result.languages).toEqual([
      { code: "en-US", default: true, enabled: true },
      { code: "de-DE", default: false, enabled: true },
    ]);
    expect(result.metadata.title).toEqual({
      "en-US": "Onboarding",
      "de-DE": "de:Onboarding",
    });
  });

  test("translates one language at a time", async () => {
    const inFlight: string[] = [];
    vi.mocked(translateFields).mockImplementation(async ({ targetLanguage, fields }) => {
      inFlight.push(targetLanguage);
      expect(inFlight).toHaveLength(1);
      await Promise.resolve();
      inFlight.pop();
      return {
        translations: Object.fromEntries(fields.map((field) => [field.path, field.defaultText])),
        failedPaths: [],
      };
    });

    await call(["ja-JP", "de-DE", "fr-FR"]);

    expect(translateFields).toHaveBeenCalledTimes(3);
  });

  test("reports each language before its call, one-based and with the total", async () => {
    vi.mocked(translateFields).mockImplementation(async ({ fields }) => ({
      translations: Object.fromEntries(fields.map((field) => [field.path, field.defaultText])),
      failedPaths: [],
    }));

    const progress: unknown[] = [];
    await translateV3SurveyPayloadLanguages({
      payload: basePayload,
      sourceLanguage: "en-US",
      targetLanguages: ["ja-JP", "de-DE"],
      organizationId: "org_1",
      workspaceId: "workspace1",
      userId: "user_1",
      onLanguageStart: (p) => progress.push(p),
    });

    expect(progress).toEqual([
      { languageCode: "ja-JP", index: 1, total: 2 },
      { languageCode: "de-DE", index: 2, total: 2 },
    ]);
  });

  test("reports a language before its call even when that call fails", async () => {
    vi.mocked(translateFields).mockRejectedValue(new Error("provider unavailable"));

    const progress: string[] = [];
    const result = await translateV3SurveyPayloadLanguages({
      payload: basePayload,
      sourceLanguage: "en-US",
      targetLanguages: ["ja-JP"],
      organizationId: "org_1",
      workspaceId: "workspace1",
      userId: "user_1",
      onLanguageStart: ({ languageCode }) => progress.push(languageCode),
    });

    // The event is the only thing on the wire during this phase, so it cannot be conditional on the
    // call succeeding — a client waiting through a failed language still needs the socket to move.
    expect(progress).toEqual(["ja-JP"]);
    expect(result.languages).toEqual([{ code: "en-US", default: true, enabled: true }]);
  });

  test("drops a language that came back only partially translated", async () => {
    vi.mocked(translateFields).mockResolvedValue({
      translations: { "metadata.title": "ja:Onboarding" },
      failedPaths: ["blocks.0.elements.0.headline"],
    });

    const result = await call(["ja-JP"]);

    // `prepareV3SurveyCreateInput` rejects a payload whose translatable fields are missing a
    // configured language, so half a language is not a language this payload may claim.
    expect(result.languages).toEqual([{ code: "en-US", default: true, enabled: true }]);
    expect(result.metadata.title).toEqual({ "en-US": "Onboarding" });
  });
});

describe("both generation routes finish the same way", () => {
  // The first version of this feature translated in the blocking route only. The product's own
  // dialog streams, so multi-language generation shipped doing nothing where it was used, and
  // every unit test still passed. This asserts the wiring the tests below cannot see.
  test("the streaming route goes through the shared finish step, not the pure builder", () => {
    const streamingOperations = readFileSync(
      new URL("../../../internal/surveys/generate/lib/operations.ts", import.meta.url),
      "utf8"
    );

    expect(streamingOperations).toContain("finishV3SurveyGeneration");
    expect(streamingOperations).not.toContain("buildV3SurveyCreatePayloadFromDraft");
  });
});
