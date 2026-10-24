import type { TFunction } from "i18next";
import { describe, expect, test } from "vite-plus/test";
import { ApiError } from "../../lib/api";
import {
  countStatusText,
  endpointTestFailure,
  isDiscordWebhook,
  triggersText,
  validWebHookURL,
  webhookCounts,
  webhookErrorMessage,
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

test("a webhook on every survey lists them all; a gone survey drops out and never widens the scope", () => {
  const surveys = [
    { id: "s1", name: "NPS" },
    { id: "s2", name: "CSAT" },
  ];
  expect(webhookSurveyNames({ surveyIds: [], allSurveys: true }, surveys)).toEqual(["NPS", "CSAT"]);
  expect(webhookSurveyNames({ surveyIds: ["s2", "gone"], allSurveys: false }, surveys)).toEqual(["CSAT"]);
  expect(webhookSurveyNames({ surveyIds: ["gone"], allSurveys: false }, surveys)).toEqual([]);
  expect(webhookSurveyNames({ surveyIds: [], allSurveys: false }, surveys)).toEqual([]);
});

test("the integrations page counts webhooks by source", () => {
  expect(
    webhookCounts([
      { source: "user" },
      { source: "zapier" },
      { source: "zapier" },
      { source: "activepieces" },
    ])
  ).toEqual({ user: 1, zapier: 2, make: 0, n8n: 0, activepieces: 1 });
  expect(countStatusText(t, 0, "webhook")).toBe("common.not_connected");
  expect(countStatusText(t, 1, "webhook")).toBe("1 webhook");
  expect(countStatusText(t, 3, "zap")).toBe("3 zaps");
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
