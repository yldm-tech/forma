import type { TFunction } from "i18next";
import { describe, expect, test } from "vite-plus/test";
import { ApiError } from "../../lib/api";
import { changeLocale, initI18n } from "../../lib/i18n";
import type { WebhookHealth } from "./api";
import {
  alertsText,
  attemptFailureText,
  connectionCountText,
  deliveryStatusBadge,
  endpointTestFailure,
  isDiscordWebhook,
  triggersText,
  validWebHookURL,
  webhookCounts,
  webhookAlerts,
  webhookErrorMessage,
  webhookHealthBadge,
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

const health = (overrides: Partial<WebhookHealth> = {}): WebhookHealth => ({
  status: "healthy",
  lastAttemptAt: "2026-10-26T00:00:00.000Z",
  lastSuccessAt: "2026-10-26T00:00:00.000Z",
  failingSince: null,
  nextAttemptAt: null,
  failedCount: 0,
  statusCode: 200,
  failure: null,
  failureMessage: null,
  ...overrides,
});

describe("delivery health", () => {
  test("a webhook is gray until something was sent to it, then green, amber while retrying, red once a delivery is lost", () => {
    expect(webhookHealthBadge(t, undefined)).toEqual({
      type: "gray",
      label: "workspace.integrations.webhooks.deliveries.health_none",
    });
    expect(webhookHealthBadge(t, health()).type).toBe("success");
    expect(webhookHealthBadge(t, health({ status: "retrying" })).type).toBe("warning");
    expect(webhookHealthBadge(t, health({ status: "failing" })).type).toBe("error");
    expect(deliveryStatusBadge(t, "succeeded").type).toBe("success");
    expect(deliveryStatusBadge(t, "retrying").type).toBe("warning");
    expect(deliveryStatusBadge(t, "failed").type).toBe("error");
  });

  test("a failed attempt says why in the user's language, with the server's reason where there is one", async () => {
    const i18n = await initI18n("en-US");
    const en = i18n.t;
    expect(attemptFailureText(en, health())).toBeNull();
    expect(
      attemptFailureText(en, health({ failure: "status", statusCode: 405, failureMessage: "Status 405" }))
    ).toBe("The endpoint answered with status 405");
    expect(
      attemptFailureText(en, health({ failure: "timeout", statusCode: null, failureMessage: "x" }))
    ).toBe("No answer within 5 seconds");
    expect(
      attemptFailureText(
        en,
        health({ failure: "unreachable", statusCode: null, failureMessage: "connection refused" })
      )
    ).toBe("Could not reach the endpoint: connection refused");
    await changeLocale(i18n, "ja-JP");
    expect(attemptFailureText(i18n.t, health({ failure: "status", statusCode: 503 }))).toBe(
      "エンドポイントがステータス 503 を返しました"
    );
  });

  test("the integrations page counts failing and retrying webhooks by source", async () => {
    const alerts = webhookAlerts([
      { source: "user", health: health({ status: "failing" }) },
      { source: "user", health: health({ status: "retrying" }) },
      { source: "user", health: health() },
      { source: "user" },
      { source: "zapier", health: health({ status: "retrying" }) },
    ]);
    expect(alerts.user).toEqual({ failing: 1, retrying: 1 });
    expect(alerts.zapier).toEqual({ failing: 0, retrying: 1 });
    expect(alerts.make).toEqual({ failing: 0, retrying: 0 });
    const i18n = await initI18n("en-US");
    const en = i18n.t;
    expect(alertsText(en, "4 webhooks", alerts.user)).toBe("4 webhooks · 1 failing · 1 retrying");
    expect(alertsText(en, "1 zap", alerts.make)).toBe("1 zap");
  });
});
