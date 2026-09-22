import { beforeEach, describe, expect, test, vi } from "vitest";

const counters = new Map<string, { add: ReturnType<typeof vi.fn> }>();
const loggerInfo = vi.fn();

vi.mock("server-only", () => ({}));

vi.mock("@opentelemetry/api", () => ({
  metrics: {
    getMeter: vi.fn(() => ({
      createCounter: vi.fn((name: string) => {
        const instrument = { add: vi.fn() };
        counters.set(name, instrument);
        return instrument;
      }),
    })),
  },
}));

vi.mock("@forma/logger", () => ({ logger: { info: loggerInfo } }));

const { recordAIGenerationUsage } = await import("./usage-metrics");

const baseRecord = {
  organizationId: "org_1",
  feature: "ai_survey_generation",
  model: "gemini-2.5-flash",
  inputTokens: 1200,
  outputTokens: 800,
  reasoningTokens: 300,
} as const;

beforeEach(() => {
  for (const instrument of counters.values()) instrument.add.mockClear();
  loggerInfo.mockClear();
});

describe("AI generation usage", () => {
  test("counts input and output as disjoint kinds so a sum over kind is the billed total", () => {
    recordAIGenerationUsage(baseRecord);

    const tokens = counters.get("forma_ai_tokens_total")?.add;
    expect(tokens).toHaveBeenCalledWith(1200, {
      feature: "ai_survey_generation",
      model: "gemini-2.5-flash",
      kind: "input",
    });
    expect(tokens).toHaveBeenCalledWith(800, {
      feature: "ai_survey_generation",
      model: "gemini-2.5-flash",
      kind: "output",
    });
    expect(tokens).toHaveBeenCalledTimes(2);
    expect(counters.get("forma_ai_generations_total")?.add).toHaveBeenCalledWith(1, {
      feature: "ai_survey_generation",
      model: "gemini-2.5-flash",
    });
  });

  test("reasoning tokens get their own counter, never a third kind that would inflate the total", () => {
    recordAIGenerationUsage(baseRecord);

    expect(counters.get("forma_ai_reasoning_tokens_total")?.add).toHaveBeenCalledWith(300, {
      feature: "ai_survey_generation",
      model: "gemini-2.5-flash",
    });
    for (const call of counters.get("forma_ai_tokens_total")?.add.mock.calls ?? []) {
      expect(call[1]).not.toMatchObject({ kind: "reasoning" });
    }
  });

  test("keeps the organization out of metric attributes and in the log line", () => {
    recordAIGenerationUsage(baseRecord);

    for (const instrument of counters.values()) {
      for (const call of instrument.add.mock.calls) {
        expect(call[1]).not.toHaveProperty("organizationId");
      }
    }

    expect(loggerInfo).toHaveBeenCalledWith(
      {
        organizationId: "org_1",
        feature: "ai_survey_generation",
        model: "gemini-2.5-flash",
        inputTokens: 1200,
        outputTokens: 800,
        reasoningTokens: 300,
      },
      "AI generation usage"
    );
  });

  test("a provider that reports no token counts still records the generation", () => {
    recordAIGenerationUsage({
      ...baseRecord,
      inputTokens: undefined,
      outputTokens: undefined,
      reasoningTokens: undefined,
    });

    expect(counters.get("forma_ai_tokens_total")?.add).not.toHaveBeenCalled();
    expect(counters.get("forma_ai_reasoning_tokens_total")?.add).not.toHaveBeenCalled();
    expect(counters.get("forma_ai_generations_total")?.add).toHaveBeenCalledTimes(1);
  });

  test("never lets an instrumentation failure escape into the generation", () => {
    counters.get("forma_ai_generations_total")?.add.mockImplementationOnce(() => {
      throw new Error("exporter unavailable");
    });

    expect(() => recordAIGenerationUsage(baseRecord)).not.toThrow();
  });

  test("a broken log transport does not take the counters down with it", () => {
    loggerInfo.mockImplementationOnce(() => {
      throw new Error("transport closed");
    });

    expect(() => recordAIGenerationUsage(baseRecord)).not.toThrow();
    expect(counters.get("forma_ai_generations_total")?.add).toHaveBeenCalledTimes(1);
  });
});
