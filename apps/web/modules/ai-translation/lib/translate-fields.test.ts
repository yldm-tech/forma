import { beforeEach, describe, expect, test, vi } from "vitest";
import { type TAITranslationField, translateFields } from "./translate-fields";

vi.mock("server-only", () => ({}));

const mockGenerateOrganizationAIObject = vi.fn();
vi.mock("@/lib/ai/service", () => ({
  generateOrganizationAIObject: (...args: unknown[]) => mockGenerateOrganizationAIObject(...args),
}));

vi.mock("@forma/logger", () => ({
  logger: { error: vi.fn(), info: vi.fn(), warn: vi.fn() },
}));

const baseInput = {
  organizationId: "org-1",
  workspaceId: "ws-1",
  userId: "user-1",
  sourceLanguage: "English",
  targetLanguage: "German",
};

const fields: TAITranslationField[] = [
  { path: "welcomeCard.headline.default", defaultText: "Welcome", isRichText: false },
  { path: "questions.0.html.default", defaultText: "<p>Hello</p>", isRichText: true },
];

const makeFields = (count: number): TAITranslationField[] =>
  Array.from({ length: count }, (_, index) => ({
    path: `questions.${index}.headline.default`,
    defaultText: `Text ${index}`,
    isRichText: false,
  }));

const mockTranslationsFor = (fieldList: TAITranslationField[]): void => {
  mockGenerateOrganizationAIObject.mockResolvedValue({
    object: Object.fromEntries(fieldList.map((_, index) => [`t${index}`, `Translated ${index}`])),
  });
};

describe("translateFields", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  test("returns translations keyed by original field paths", async () => {
    mockGenerateOrganizationAIObject.mockResolvedValue({
      object: { t0: "Willkommen", t1: "<p>Hallo</p>" },
    });

    const result = await translateFields({ ...baseInput, fields });

    expect(result).toEqual({
      translations: {
        "welcomeCard.headline.default": "Willkommen",
        "questions.0.html.default": "<p>Hallo</p>",
      },
      failedPaths: [],
    });
  });

  test("sends opaque indexed IDs to the model, never the field paths", async () => {
    mockGenerateOrganizationAIObject.mockResolvedValue({
      object: { t0: "Willkommen", t1: "<p>Hallo</p>" },
    });

    await translateFields({ ...baseInput, fields });

    expect(mockGenerateOrganizationAIObject).toHaveBeenCalledTimes(1);
    const callArg = mockGenerateOrganizationAIObject.mock.calls[0][0];
    expect(callArg.prompt).not.toContain("welcomeCard.headline.default");
    expect(callArg.prompt).not.toContain("questions.0.html.default");
    const userPayload = JSON.parse(callArg.prompt);
    expect(userPayload).toEqual([
      { id: "t0", text: "Welcome", richText: false },
      { id: "t1", text: "<p>Hello</p>", richText: true },
    ]);
  });

  test("requests deterministic output (temperature: 0) for stable translations", async () => {
    mockGenerateOrganizationAIObject.mockResolvedValue({
      object: { t0: "Willkommen", t1: "<p>Hallo</p>" },
    });

    await translateFields({ ...baseInput, fields });

    expect(mockGenerateOrganizationAIObject.mock.calls[0][0]).toMatchObject({
      temperature: 0,
      maxOutputTokens: 1024,
      timeout: 45000,
    });
  });

  test("scales maxOutputTokens with field count in the mid-range", async () => {
    const midRangeFields = makeFields(20);
    mockTranslationsFor(midRangeFields);

    await translateFields({ ...baseInput, fields: midRangeFields });

    expect(mockGenerateOrganizationAIObject.mock.calls[0][0]).toMatchObject({
      maxOutputTokens: 3200,
    });
  });

  test("splits a batch larger than one call's budget into sequential chunks", async () => {
    // 51 = the 8192-token output cap divided by the 160-token per-field budget the module declares.
    // Above that the model is asked for a reply it is not allowed to finish, so the request is split
    // instead of being clamped into a truncated response.
    const largeBatchFields = makeFields(60);
    mockTranslationsFor(largeBatchFields);

    await translateFields({ ...baseInput, fields: largeBatchFields });

    expect(mockGenerateOrganizationAIObject).toHaveBeenCalledTimes(2);
    expect(JSON.parse(mockGenerateOrganizationAIObject.mock.calls[0][0].prompt)).toHaveLength(51);
    expect(JSON.parse(mockGenerateOrganizationAIObject.mock.calls[1][0].prompt)).toHaveLength(9);
    expect(mockGenerateOrganizationAIObject.mock.calls[0][0]).toMatchObject({ maxOutputTokens: 8160 });
    expect(mockGenerateOrganizationAIObject.mock.calls[1][0]).toMatchObject({ maxOutputTokens: 1440 });
  });

  test("runs chunks one at a time rather than in parallel", async () => {
    const largeBatchFields = makeFields(60);
    let inFlight = 0;
    mockGenerateOrganizationAIObject.mockImplementation(async (args: { prompt: string }) => {
      inFlight += 1;
      expect(inFlight).toBe(1);
      await Promise.resolve();
      inFlight -= 1;
      const items = JSON.parse(args.prompt) as { id: string }[];
      return { object: Object.fromEntries(items.map((item) => [item.id, `translated ${item.id}`])) };
    });

    await translateFields({ ...baseInput, fields: largeBatchFields });

    expect(mockGenerateOrganizationAIObject).toHaveBeenCalledTimes(2);
  });

  test("keeps the chunks that came back when one chunk's call fails", async () => {
    const largeBatchFields = makeFields(60);
    mockGenerateOrganizationAIObject
      .mockRejectedValueOnce(new Error("provider unavailable"))
      .mockImplementationOnce(async (args: { prompt: string }) => {
        const items = JSON.parse(args.prompt) as { id: string }[];
        return { object: Object.fromEntries(items.map((item) => [item.id, `translated ${item.id}`])) };
      });

    const result = await translateFields({ ...baseInput, fields: largeBatchFields });

    // The nine fields of the second chunk survive the first chunk's failure; before chunking, one
    // provider error cost every field in the batch.
    expect(Object.keys(result.translations)).toHaveLength(9);
    expect(result.failedPaths).toHaveLength(51);
    expect(result.failedPaths).toContain("questions.0.headline.default");
  });

  test("returns empty object without calling the model when no fields are provided", async () => {
    const result = await translateFields({ ...baseInput, fields: [] });

    expect(result).toEqual({ translations: {}, failedPaths: [] });
    expect(mockGenerateOrganizationAIObject).not.toHaveBeenCalled();
  });

  test("keeps what came back and names the paths that did not when the model omits an ID", async () => {
    mockGenerateOrganizationAIObject.mockResolvedValue({
      object: { t0: "Willkommen" }, // t1 missing
    });

    const result = await translateFields({ ...baseInput, fields });

    expect(result).toEqual({
      translations: { "welcomeCard.headline.default": "Willkommen" },
      failedPaths: ["questions.0.html.default"],
    });
  });

  test("treats an empty string as a field that did not translate", async () => {
    mockGenerateOrganizationAIObject.mockResolvedValue({
      object: { t0: "Willkommen", t1: "" },
    });

    const result = await translateFields({ ...baseInput, fields });

    expect(result.translations).toEqual({ "welcomeCard.headline.default": "Willkommen" });
    expect(result.failedPaths).toEqual(["questions.0.html.default"]);
  });

  test("throws rather than reporting an empty success when no field translated at all", async () => {
    // A partial result the caller can keep and an empty one it cannot are different outcomes, and
    // the second still has to reach the caller as the failure it is.
    mockGenerateOrganizationAIObject.mockResolvedValue({ object: {} });

    await expect(translateFields({ ...baseInput, fields })).rejects.toThrow(
      "AI translation returned incomplete result"
    );
  });

  test("propagates errors thrown by the AI provider", async () => {
    // Rethrown rather than flattened: the code on it (quota, timeout) is what the caller maps to a
    // message, and a chunked run must not swallow it into the generic incomplete-result error.
    mockGenerateOrganizationAIObject.mockRejectedValue(new Error("provider failed"));

    await expect(translateFields({ ...baseInput, fields })).rejects.toThrow("provider failed");
  });

  test("echoes empty defaultText through without calling the model", async () => {
    const allEmpty: TAITranslationField[] = [
      { path: "welcomeCard.subheader.default", defaultText: "", isRichText: false },
      { path: "endings.0.subheader.default", defaultText: "", isRichText: false },
    ];

    const result = await translateFields({ ...baseInput, fields: allEmpty });

    expect(result).toEqual({
      translations: {
        "welcomeCard.subheader.default": "",
        "endings.0.subheader.default": "",
      },
      failedPaths: [],
    });
    expect(mockGenerateOrganizationAIObject).not.toHaveBeenCalled();
  });

  test("translates non-empty fields and echoes empty ones in the same call", async () => {
    const mixed: TAITranslationField[] = [
      { path: "welcomeCard.headline.default", defaultText: "Welcome", isRichText: false },
      { path: "welcomeCard.subheader.default", defaultText: "", isRichText: false },
      { path: "questions.0.headline.default", defaultText: "How are you?", isRichText: false },
    ];

    // Empty fields are filtered out before indexing, so the model only sees
    // the two non-empty entries as t0 and t1.
    mockGenerateOrganizationAIObject.mockResolvedValue({
      object: { t0: "Willkommen", t1: "Wie geht es dir?" },
    });

    const result = await translateFields({ ...baseInput, fields: mixed });

    expect(result).toEqual({
      translations: {
        "welcomeCard.headline.default": "Willkommen",
        "welcomeCard.subheader.default": "",
        "questions.0.headline.default": "Wie geht es dir?",
      },
      failedPaths: [],
    });

    // Confirm the model never saw the empty field in the payload.
    const callArg = mockGenerateOrganizationAIObject.mock.calls[0][0];
    const userPayload = JSON.parse(callArg.prompt);
    expect(userPayload).toEqual([
      { id: "t0", text: "Welcome", richText: false },
      { id: "t1", text: "How are you?", richText: false },
    ]);
  });
});
