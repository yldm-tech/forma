import type { TFunction } from "i18next";
import { describe, expect, test } from "vite-plus/test";
import { ApiError } from "../../lib/api";
import { changeLocale, initI18n } from "../../lib/i18n";
import {
  connectionCountText,
  endpointTestFailure,
  isDiscordWebhook,
  triggersText,
  validWebHookURL,
  webhookCounts,
  webhookErrorMessage,
  webhookSourceLabel,
  webhookSurveyNames,
} from "./webhooks";

const t = ((key: string) => key) as unknown as TFunction;

describe("validWebHookURL", () => {
  test("needs a URL", () => {
    expect(validWebHookURL("  ")).toEqual({ valid: false, error: "Please enter a URL" });
    expect(validWebHookURL("not a url")).toEqual({
      valid: false,
      error: "Invalid URL format. Please enter a complete URL including https://",
    });
  });

  test("accepts https with a real domain", () => {
    expect(validWebHookURL("https://hooks.example.com/forma")).toEqual({ valid: true });
    expect(validWebHookURL("  https://example.io/a  ")).toEqual({ valid: true });
  });

  test("refuses http, doubled slashes, IP literals and single-label hosts", () => {
    expect(validWebHookURL("http://example.com")).toEqual({
      valid: false,
      error: "URL must start with https://",
    });
    const domainError = "Please enter a complete URL with a valid domain (e.g., https://forma.yldm.ai)";
    expect(validWebHookURL("https://example.com//hook")).toEqual({ valid: false, error: domainError });
    expect(validWebHookURL("https://10.0.0.1/hook")).toEqual({ valid: false, error: domainError });
    expect(validWebHookURL("https://intranet/hook")).toEqual({ valid: false, error: domainError });
  });

  test("lets http and internal hosts through when the instance allows internal URLs", () => {
    expect(validWebHookURL("http://localhost:8080/hook", true)).toEqual({ valid: true });
    expect(validWebHookURL("ftp://example.com", true)).toEqual({
      valid: false,
      error: "URL must start with https:// or http://",
    });
  });
});

test("Discord's webhook URLs are recognised", () => {
  expect(isDiscordWebhook("https://discord.com/api/webhooks/123/abc")).toBe(true);
  expect(isDiscordWebhook("https://discord.com/api/webhooks/abc/def")).toBe(false);
  expect(isDiscordWebhook("https://example.com/api/webhooks/123/abc")).toBe(false);
  expect(isDiscordWebhook("not a url")).toBe(false);
});

test("triggers are listed in the order they happen", () => {
  expect(triggersText(t, ["responseFinished", "responseCreated"])).toBe(
    "workspace.integrations.webhooks.response_created, workspace.integrations.webhooks.response_finished"
  );
});

test("a webhook on every survey lists none, since it covers surveys not created yet; a gone survey drops out and never widens the scope", () => {
  const surveys = [
    { id: "s1", name: "NPS" },
    { id: "s2", name: "CSAT" },
  ];
  expect(webhookSurveyNames({ surveyIds: [], allSurveys: true }, surveys)).toBeNull();
  expect(webhookSurveyNames({ surveyIds: ["s1"], allSurveys: true }, surveys)).toBeNull();
  expect(webhookSurveyNames({ surveyIds: ["s2", "gone"], allSurveys: false }, surveys)).toEqual(["CSAT"]);
  expect(webhookSurveyNames({ surveyIds: ["gone"], allSurveys: false }, surveys)).toEqual([]);
  expect(webhookSurveyNames({ surveyIds: [], allSurveys: false }, surveys)).toEqual([]);
});

test("the integrations page counts webhooks by source, in the user's language", async () => {
  expect(
    webhookCounts([
      { source: "user" },
      { source: "zapier" },
      { source: "zapier" },
      { source: "activepieces" },
    ])
  ).toEqual({ user: 1, zapier: 2, make: 0, n8n: 0, activepieces: 1 });
  const i18n = await initI18n("en-US");
  expect(connectionCountText(i18n.t, 0, "webhook")).toBe(i18n.t("common.not_connected"));
  expect(connectionCountText(i18n.t, 1, "webhook")).toBe("1 webhook");
  expect(connectionCountText(i18n.t, 3, "zap")).toBe("3 zaps");
  expect(connectionCountText(i18n.t, 2, "integration")).toBe("2 integrations");
  await changeLocale(i18n, "ja-JP");
  for (const noun of ["webhook", "zap", "integration"] as const) {
    const text = connectionCountText(i18n.t, 2, noun);
    expect(text).toContain("2");
    expect(text).not.toMatch(/webhooks|zaps|integrations|workspace\./);
  }
});

test("a webhook's source is the user's in their language, or a platform by its product name", () => {
  expect(webhookSourceLabel(t, "user")).toBe("common.user");
  expect(webhookSourceLabel(t, "n8n")).toBe("n8n");
  expect(webhookSourceLabel(t, "activepieces")).toBe("Activepieces");
});

test("an endpoint refusal forma2 translated is shown in the user's language, anything else as the server says", () => {
  expect(webhookErrorMessage(t, new ApiError(400, "endpoint_not_found_error", "Not Found (404)"))).toBe(
    "workspace.integrations.webhooks.endpoint_not_found_error"
  );
  expect(
    webhookErrorMessage(t, new ApiError(400, "endpoint_timeout_error", "Request timed out after 5 seconds"))
  ).toBe("Request timed out after 5 seconds");
  expect(webhookErrorMessage(t, new ApiError(500, "internal_error", "boom"))).toBe(
    "common.something_went_wrong_please_try_again"
  );
});

test("a reason too long for a toast points to the console", () => {
  expect(endpointTestFailure(t, "Not Found")).toBe(
    "workspace.integrations.webhooks.endpoint_pinged_error \n Not Found"
  );
  expect(endpointTestFailure(t, "x".repeat(250))).toBe(
    "workspace.integrations.webhooks.endpoint_pinged_error \n workspace.integrations.webhooks.please_check_console"
  );
});
