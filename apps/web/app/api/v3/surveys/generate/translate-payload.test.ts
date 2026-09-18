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
    vi.mocked(translateFields).mockImplementation(async ({ fields }) =>
      Object.fromEntries(fields.map((field) => [field.path, `ja:${field.defaultText}`]))
    );

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
    vi.mocked(translateFields).mockResolvedValue({ "metadata.title": "translated" });

    await call(["ja-JP"]);

    expect(basePayload.metadata.title).toEqual({ "en-US": "Onboarding" });
  });

  test("keeps the languages that translated when one of them fails", async () => {
    vi.mocked(translateFields)
      .mockRejectedValueOnce(new Error("provider unavailable"))
      .mockImplementationOnce(async ({ fields }) =>
        Object.fromEntries(fields.map((field) => [field.path, `de:${field.defaultText}`]))
      );

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
      return Object.fromEntries(fields.map((field) => [field.path, field.defaultText]));
    });

    await call(["ja-JP", "de-DE", "fr-FR"]);

    expect(translateFields).toHaveBeenCalledTimes(3);
  });
});
