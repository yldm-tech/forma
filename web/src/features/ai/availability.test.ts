import { afterEach, describe, expect, test, vi } from "vite-plus/test";
import {
  getAIUnavailableMessage,
  getAIUnavailableMessageForErrorCode,
  offersEnableAI,
  translateSurveyFields,
  workspaceAIQuery,
} from "./availability";

// Echoing the key back keeps the assertions about which string each input resolves to, without pinning the English wording.
const t = (key: string) => key;

afterEach(() => {
  vi.unstubAllGlobals();
});

// forma2's lib/ai/availability.test.ts.
describe("offersEnableAI", () => {
  test("sends people to organization settings only when AI is switched off", () => {
    expect(offersEnableAI("not_enabled")).toBe(true);
    expect(offersEnableAI("instance_not_configured")).toBe(false);
    expect(offersEnableAI("read_only")).toBe(false);
    expect(offersEnableAI("not_in_plan")).toBe(false);
    expect(offersEnableAI(undefined)).toBe(false);
  });
});

describe("getAIUnavailableMessage", () => {
  test("resolves one shared message per reason", () => {
    expect(getAIUnavailableMessage("not_in_plan", t)).toBe("common.ai_unavailable.not_in_plan");
    expect(getAIUnavailableMessage("not_enabled", t)).toBe("common.ai_unavailable.not_enabled");
    expect(getAIUnavailableMessage("instance_not_configured", t)).toBe(
      "common.ai_unavailable.instance_not_configured"
    );
    expect(getAIUnavailableMessage("read_only", t)).toBe("common.ai_unavailable.read_only");
  });

  test("falls back to the generic message when the reason is unknown", () => {
    expect(getAIUnavailableMessage(undefined, t)).toBe("common.ai_unavailable.unknown");
  });
});

describe("getAIUnavailableMessageForErrorCode", () => {
  test("maps the AI-unavailable server codes onto the reason copy", () => {
    expect(getAIUnavailableMessageForErrorCode("ai_features_not_enabled", t)).toBe(
      getAIUnavailableMessage("not_in_plan", t)
    );
    expect(getAIUnavailableMessageForErrorCode("ai_smart_tools_disabled", t)).toBe(
      getAIUnavailableMessage("not_enabled", t)
    );
    expect(getAIUnavailableMessageForErrorCode("ai_instance_not_configured", t)).toBe(
      getAIUnavailableMessage("instance_not_configured", t)
    );
  });

  test("returns undefined for codes the caller must handle itself", () => {
    expect(getAIUnavailableMessageForErrorCode("ai_quota_exceeded", t)).toBeUndefined();
    expect(getAIUnavailableMessageForErrorCode("toString", t)).toBeUndefined();
    expect(getAIUnavailableMessageForErrorCode("", t)).toBeUndefined();
    expect(getAIUnavailableMessageForErrorCode(undefined, t)).toBeUndefined();
  });
});

describe("the AI routes", () => {
  test("reads a workspace's availability and posts a translation", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(Response.json({ available: false, reason: "read_only" }))
      .mockResolvedValueOnce(Response.json({ translations: { "a.b": "Hallo" }, failedPaths: [] }));
    vi.stubGlobal("fetch", fetchMock);

    const query = workspaceAIQuery("ws1");
    await expect(
      (query.queryFn as (ctx: { signal: AbortSignal }) => Promise<unknown>)({
        signal: new AbortController().signal,
      })
    ).resolves.toEqual({ available: false, reason: "read_only" });
    expect(fetchMock.mock.calls[0]?.[0]).toBe("/api/app/workspaces/ws1/ai");

    const fields = [{ path: "a.b", defaultText: "Hello", isRichText: false }];
    await expect(
      translateSurveyFields("ws1", { fields, sourceLanguage: "English", targetLanguage: "Deutsch" })
    ).resolves.toEqual({ translations: { "a.b": "Hallo" }, failedPaths: [] });
    const [path, init] = fetchMock.mock.calls[1] as [string, RequestInit & { body: string }];
    expect(path).toBe("/api/app/workspaces/ws1/ai/translations");
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body)).toEqual({ fields, sourceLanguage: "English", targetLanguage: "Deutsch" });
  });
});
