import type { TFunction } from "i18next";
import { ApiError } from "../../lib/api";
import { errorMessage } from "../../lib/errors";
import type { Webhook, WebhookSource, WebhookTrigger } from "./api";

// The webhooks page's rules, forma2's modules/integrations/webhooks/lib/utils.ts and the table's display helpers.

/** forma2's validWebHookURL, the form's own check before the server's: https with a real domain, unless the instance allows internal URLs, when http and any host will do. */
export const validWebHookURL = (
  urlInput: string,
  allowInternalUrls = false
): { valid: true } | { valid: false; error: string } => {
  const trimmedInput = urlInput.trim();
  if (!trimmedInput) return { valid: false, error: "Please enter a URL" };
  let url: URL;
  try {
    url = new URL(trimmedInput);
  } catch {
    return { valid: false, error: "Invalid URL format. Please enter a complete URL including https://" };
  }
  if (allowInternalUrls) {
    if (url.protocol !== "https:" && url.protocol !== "http:") {
      return { valid: false, error: "URL must start with https:// or http://" };
    }
    return { valid: true };
  }
  if (url.protocol !== "https:") return { valid: false, error: "URL must start with https://" };
  const domainError = "Please enter a complete URL with a valid domain (e.g., https://forma.yldm.ai)";
  if (/(?<!:)\/\/+/.test(trimmedInput)) return { valid: false, error: domainError };
  if (!/^[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$/.test(url.hostname)) return { valid: false, error: domainError };
  return { valid: true };
};

/** forma2's isDiscordWebhook: Discord's webhooks expect their own payload, so they are refused. */
export const isDiscordWebhook = (urlString: string): boolean => {
  try {
    return /^https:\/\/discord\.com\/api\/webhooks\/\d+\/.+$/.test(new URL(urlString).toString());
  } catch {
    return false;
  }
};

export const WEBHOOK_TRIGGERS: readonly WebhookTrigger[] = [
  "responseCreated",
  "responseUpdated",
  "responseFinished",
];

export const triggerLabel = (t: TFunction, trigger: WebhookTrigger): string => {
  switch (trigger) {
    case "responseCreated":
      return t("workspace.integrations.webhooks.response_created");
    case "responseUpdated":
      return t("workspace.integrations.webhooks.response_updated");
    case "responseFinished":
      return t("workspace.integrations.webhooks.response_finished");
  }
};

/** A webhook's triggers as the table lists them: in the order they happen. */
export const triggersText = (t: TFunction, triggers: readonly WebhookTrigger[]): string =>
  WEBHOOK_TRIGGERS.filter((trigger) => triggers.includes(trigger))
    .map((trigger) => triggerLabel(t, trigger))
    .join(", ");

/** The names of the surveys a webhook lists that still exist (a survey that is gone drops out); null for a webhook scoped to all surveys, which covers surveys created later too and so is described rather than listed. */
export const webhookSurveyNames = (
  webhook: Pick<Webhook, "surveyIds" | "allSurveys">,
  surveys: readonly { id: string; name: string }[]
): string[] | null => {
  if (webhook.allSurveys) return null;
  return webhook.surveyIds.flatMap((id) => {
    const survey = surveys.find((s) => s.id === id);
    return survey ? [survey.name] : [];
  });
};

/** Who created a webhook, as the list shows it: the automation platforms by their product names, which are not translated, and a webhook added on this page as the user's. */
export const webhookSourceLabel = (t: TFunction, source: WebhookSource): string => {
  switch (source) {
    case "user":
      return t("common.user");
    case "zapier":
      return "Zapier";
    case "make":
      return "Make";
    case "n8n":
      return "n8n";
    case "activepieces":
      return "Activepieces";
  }
};

/** How many webhooks of each source a workspace has, for the integrations page's cards. */
export const webhookCounts = (
  webhooks: readonly Pick<Webhook, "source">[]
): Record<WebhookSource, number> => {
  const counts: Record<WebhookSource, number> = { user: 0, zapier: 0, make: 0, n8n: 0, activepieces: 0 };
  for (const webhook of webhooks) counts[webhook.source] += 1;
  return counts;
};

/** forma2's getStatusText on the integrations page: "1 webhook", "3 webhooks", or not connected, counted in the user's language. */
export const connectionCountText = (
  t: TFunction,
  count: number,
  noun: "webhook" | "zap" | "integration"
): string => {
  if (count === 0) return t("common.not_connected");
  switch (noun) {
    case "webhook":
      return t("workspace.integrations.webhook_count", { count });
    case "zap":
      return t("workspace.integrations.zap_count", { count });
    case "integration":
      return t("workspace.integrations.integration_count", { count });
  }
};

/** The endpoint test's refusals forma2 translated; the server sends the key as the code. */
const TRANSLATED_TEST_ERRORS = new Set([
  "endpoint_internal_server_error",
  "endpoint_not_found_error",
  "endpoint_method_not_allowed_error",
  "endpoint_bad_gateway_error",
  "endpoint_service_unavailable_error",
  "endpoint_gateway_timeout_error",
  "discord_webhook_not_supported",
]);

/** The message of a failed webhook request: a refusal forma2 translated in the user's language, any other expected error with the server's message. */
export const webhookErrorMessage = (t: TFunction, error: unknown): string => {
  if (error instanceof ApiError && TRANSLATED_TEST_ERRORS.has(error.code)) {
    return t(`workspace.integrations.webhooks.${error.code}`);
  }
  return errorMessage(t, error);
};

/** forma2's endpoint-test toast: the reason, or a pointer to the console when it is too long to read in a toast. */
export const endpointTestFailure = (t: TFunction, message: string): string =>
  `${t("workspace.integrations.webhooks.endpoint_pinged_error")} \n ${
    message.length < 250 ? message : t("workspace.integrations.webhooks.please_check_console")
  }`;
