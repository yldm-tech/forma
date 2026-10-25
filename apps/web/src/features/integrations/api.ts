import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import type { TI18nString } from "@forma/types/i18n";
import { api } from "../../lib/api";
import { surveysApi } from "../surveys/api";

// The integrations pages' endpoints: /api/app/workspaces/{id}/integrations/* and /api/app/integrations/{id} (apps/server/internal/integration/handler.go), and the webhooks page's /api/app/workspaces/{id}/webhooks and /api/app/webhooks/{id} (apps/server/internal/webhook/handler.go).

export type IntegrationType = "googleSheets" | "airtable" | "notion" | "slack";

/** What every link carries, forma2's ZIntegrationBaseSurveyData without the element selection. A flag that is absent counts as off. */
export interface LinkBase {
  createdAt: string;
  surveyId: string;
  surveyName: string;
  includeVariables?: boolean;
  includeHiddenFields?: boolean;
  includeMetadata?: boolean;
  includeCreatedAt?: boolean;
  includeContactAttributes?: boolean;
}

/** The element selection of a Google Sheets, Airtable or Slack link, and the label the tables show for it. */
export interface ElementSelection {
  elementIds: string[];
  elements: string;
}

export interface SheetLink extends LinkBase, ElementSelection {
  spreadsheetId: string;
  spreadsheetName: string;
}

export interface AirtableLink extends LinkBase, ElementSelection {
  baseId: string;
  tableId: string;
  tableName: string;
}

export interface SlackLink extends LinkBase, ElementSelection {
  channelId: string;
  channelName: string;
}

/** One side of a Notion mapping entry: an element (or metadata, createdAt, person.<key>, a hidden field or variable storage key) or a database property. */
export interface NotionRef {
  id: string;
  name: string;
  type: string;
}

export interface NotionMapping {
  element: NotionRef;
  column: NotionRef;
}

export interface NotionLink extends LinkBase {
  mapping: NotionMapping[];
  databaseId: string;
  databaseName: string;
}

export interface LinkOf {
  googleSheets: SheetLink;
  airtable: AirtableLink;
  notion: NotionLink;
  slack: SlackLink;
}

/** Who an integration is connected as; never its tokens. */
export interface Connection {
  connected: boolean;
  email?: string;
  workspaceName?: string | null;
  teamName?: string;
}

export interface Integration<T extends IntegrationType = IntegrationType> {
  id: string;
  type: T;
  config: { data: LinkOf[T][] };
  connection: Connection;
  createdAt: string;
  updatedAt: string;
  lastErrorAt: string | null;
  lastErrorMessage: string | null;
  consecutiveFailures: number;
}

export interface IntegrationsOverview {
  integrations: Integration[];
  /** Which integrations this instance has OAuth app credentials for; the Connect button is disabled otherwise. */
  enabled: Record<IntegrationType, boolean>;
}

export interface LinkableElement {
  id: string;
  type: string;
  headline: TI18nString;
}

/** A survey the link modals offer (not completed, not archived), with what a link can select of it. */
export interface LinkableSurvey {
  id: string;
  name: string;
  elements: LinkableElement[];
  /** The storage keys of its hidden fields. */
  hiddenFields: string[];
  variables: { storageKey: string; name: string }[];
}

/** A base, table or channel. */
export interface IntegrationItem {
  id: string;
  name: string;
}

export interface NotionDatabase {
  id: string;
  name: string;
  properties: NotionRef[];
}

export const integrationKeys = {
  all: (workspaceId: string) => ["workspaces", workspaceId, "integrations"] as const,
  surveys: (workspaceId: string) => ["workspaces", workspaceId, "integrations", "surveys"] as const,
  googleSheetsValidation: (workspaceId: string) =>
    ["workspaces", workspaceId, "integrations", "googleSheets", "validate"] as const,
  airtableBases: (workspaceId: string) =>
    ["workspaces", workspaceId, "integrations", "airtable", "bases"] as const,
  airtableTables: (workspaceId: string, baseId: string) =>
    ["workspaces", workspaceId, "integrations", "airtable", "bases", baseId, "tables"] as const,
  notionDatabases: (workspaceId: string) =>
    ["workspaces", workspaceId, "integrations", "notion", "databases"] as const,
  slackChannels: (workspaceId: string) =>
    ["workspaces", workspaceId, "integrations", "slack", "channels"] as const,
};

const base = (workspaceId: string) => `/api/app/workspaces/${workspaceId}/integrations`;

export const integrationsQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: integrationKeys.all(workspaceId),
    queryFn: ({ signal }) => api.get<IntegrationsOverview>(base(workspaceId), { signal }),
  });

/** The workspace's integration of the type, when it has one. */
export const integrationOf = <T extends IntegrationType>(
  overview: IntegrationsOverview,
  type: T
): Integration<T> | undefined =>
  overview.integrations.find((integration): integration is Integration<T> => integration.type === type);

export const linkableSurveysQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: integrationKeys.surveys(workspaceId),
    queryFn: ({ signal }) => api.get<LinkableSurvey[]>(`${base(workspaceId)}/surveys`, { signal }),
  });

/** forma2's validateGoogleSheetsConnectionAction: false without an integration; invalid_grant when the credential cannot be refreshed. */
export const googleSheetsValidationQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: integrationKeys.googleSheetsValidation(workspaceId),
    queryFn: ({ signal }) =>
      api
        .get<{ connected: boolean }>(`${base(workspaceId)}/googleSheets/validate`, { signal })
        .then((res) => res.connected),
    retry: false,
  });

export const airtableBasesQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: integrationKeys.airtableBases(workspaceId),
    queryFn: ({ signal }) => api.get<IntegrationItem[]>(`${base(workspaceId)}/airtable/bases`, { signal }),
    retry: false,
  });

export const airtableTablesQuery = (workspaceId: string, baseId: string) =>
  queryOptions({
    queryKey: integrationKeys.airtableTables(workspaceId, baseId),
    queryFn: ({ signal }) =>
      api.get<IntegrationItem[]>(`${base(workspaceId)}/airtable/bases/${encodeURIComponent(baseId)}/tables`, {
        signal,
      }),
  });

export const notionDatabasesQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: integrationKeys.notionDatabases(workspaceId),
    queryFn: ({ signal }) => api.get<NotionDatabase[]>(`${base(workspaceId)}/notion/databases`, { signal }),
  });

export const slackChannelsQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: integrationKeys.slackChannels(workspaceId),
    queryFn: ({ signal }) => api.get<IntegrationItem[]>(`${base(workspaceId)}/slack/channels`, { signal }),
    retry: false,
  });

/** The provider's authorize URL for the workspace, carrying a state bound to this session; the page sends the browser there. */
export const connectIntegration = (workspaceId: string, type: IntegrationType) =>
  api.post<{ authUrl: string }>(`${base(workspaceId)}/${type}/connect`).then((res) => res.authUrl);

/** forma2's getSpreadsheetNameByIdAction. */
export const spreadsheetName = (workspaceId: string, spreadsheetId: string) =>
  api
    .get<{ name: string }>(
      `${base(workspaceId)}/googleSheets/spreadsheets/${encodeURIComponent(spreadsheetId)}`
    )
    .then((res) => res.name);

/** forma2's createOrUpdateIntegrationAction: every link of the integration, as the modal now has them. */
export const useSaveLinks = <T extends IntegrationType>(workspaceId: string, type: T) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (links: LinkOf[T][]) =>
      api.put<{ id: string }>(`${base(workspaceId)}/${type}`, { data: links }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: integrationKeys.all(workspaceId) }),
  });
};

/** forma2's deleteIntegrationAction: the integration with its links and credentials. */
export const useDeleteIntegration = (workspaceId: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (integrationId: string) => api.delete<void>(`/api/app/integrations/${integrationId}`),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: integrationKeys.all(workspaceId) }),
  });
};

// ---- webhooks ----

export type WebhookTrigger = "responseCreated" | "responseUpdated" | "responseFinished";
export type WebhookSource = "user" | "zapier" | "make" | "n8n" | "activepieces";

/** A webhook as the settings page sees it: never with its secret, except in the answer to its creation. */
export interface Webhook {
  id: string;
  name: string | null;
  createdAt: string;
  updatedAt: string;
  url: string;
  source: WebhookSource;
  workspaceId: string;
  triggers: WebhookTrigger[];
  /** The surveys the webhook sends for, deleted ones included; empty when allSurveys is set. */
  surveyIds: string[];
  /** The webhook sends for every current and future survey. When unset it sends only for surveyIds, even once they are all deleted. */
  allSurveys: boolean;
  secret?: string;
  /** Its delivery health, in the list only; absent while nothing was sent to it. */
  health?: WebhookHealth;
}

/** healthy: the latest attempt succeeded. retrying: it failed, and every delivery since the last success is still being retried. failing: a delivery since the last success ran out of retries. */
export type WebhookHealthStatus = "healthy" | "retrying" | "failing";

/** Why an attempt failed: an answer outside 2xx, a redirect (never followed), no answer within 5 seconds, no connection, or a URL the server refuses to send to. */
export type WebhookDeliveryFailure = "status" | "redirect" | "timeout" | "unreachable" | "refused";

/** The outcome of the latest attempt of a delivery, or of a webhook's latest attempt. */
export interface WebhookAttemptOutcome {
  statusCode: number | null;
  failure: WebhookDeliveryFailure | null;
  failureMessage: string | null;
}

export interface WebhookHealth extends WebhookAttemptOutcome {
  status: WebhookHealthStatus;
  lastAttemptAt: string;
  lastSuccessAt: string | null;
  /** The first failed attempt after the last success. */
  failingSince: string | null;
  /** When the latest attempt's delivery is tried again. */
  nextAttemptAt: string | null;
  /** The deliveries that ran out of retries since the last success. */
  failedCount: number;
}

export type WebhookDeliveryStatus = "succeeded" | "retrying" | "failed";

/** One event sent to a webhook, with the outcome of its latest attempt; its payload stays on the server. */
export interface WebhookDelivery extends WebhookAttemptOutcome {
  id: string;
  createdAt: string;
  event: WebhookTrigger;
  responseId: string;
  surveyId: string;
  surveyName: string | null;
  /** The webhook-id header of every attempt, which receivers deduplicate on. */
  messageId: string;
  status: WebhookDeliveryStatus;
  attempts: number;
  lastAttemptAt: string;
  nextAttemptAt: string | null;
  latencyMs: number;
}

export interface WebhookInput {
  name: string | null;
  url: string;
  source: WebhookSource;
  triggers: WebhookTrigger[];
  surveyIds: string[];
  allSurveys: boolean;
}

export interface WebhooksList {
  webhooks: Webhook[];
  /** DANGEROUSLY_ALLOW_WEBHOOK_INTERNAL_URLS: the URL field accepts http and internal hosts. */
  allowInternalUrls: boolean;
}

export const webhookKeys = {
  all: (workspaceId: string) => ["workspaces", workspaceId, "webhooks"] as const,
  surveys: (workspaceId: string) => ["workspaces", workspaceId, "webhooks", "surveys"] as const,
  deliveries: (workspaceId: string, webhookId: string) =>
    ["workspaces", workspaceId, "webhooks", webhookId, "deliveries"] as const,
};

/** The surveys a webhook can be scoped to: forma2's page loaded the workspace's 200 most recently updated. */
export const webhookSurveysQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: webhookKeys.surveys(workspaceId),
    queryFn: ({ signal }) =>
      surveysApi
        .list(
          { workspaceId, limit: 200, filters: { name: "", status: [], type: [], sortBy: "updatedAt" } },
          signal
        )
        .then((page) => page.data.map(({ id, name }) => ({ id, name }))),
  });

export const webhooksQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: webhookKeys.all(workspaceId),
    queryFn: ({ signal }) => api.get<WebhooksList>(`/api/app/workspaces/${workspaceId}/webhooks`, { signal }),
  });

/** forma2's testEndpointAction: one signed test request to url. For a webhook being edited its stored secret signs it; for one being created the answer carries the secret it was signed with, for the create that follows. */
export const testWebhookEndpoint = (
  workspaceId: string,
  input: { url: string; webhookId?: string; secret?: string }
) =>
  api.post<{ success: boolean; secret?: string }>(`/api/app/workspaces/${workspaceId}/webhooks/test`, input);

export const useCreateWebhook = (workspaceId: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: WebhookInput & { webhookSecret?: string }) =>
      api.post<Webhook>(`/api/app/workspaces/${workspaceId}/webhooks`, input),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: webhookKeys.all(workspaceId) }),
  });
};

export const useUpdateWebhook = (workspaceId: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ webhookId, input }: { webhookId: string; input: WebhookInput }) =>
      api.patch<Webhook>(`/api/app/webhooks/${webhookId}`, input),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: webhookKeys.all(workspaceId) }),
  });
};

export const useDeleteWebhook = (workspaceId: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (webhookId: string) => api.delete<void>(`/api/app/webhooks/${webhookId}`),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: webhookKeys.all(workspaceId) }),
  });
};

/** A webhook's delivery log: its last 100 events, newest first. */
export const webhookDeliveriesQuery = (workspaceId: string, webhookId: string) =>
  queryOptions({
    queryKey: webhookKeys.deliveries(workspaceId, webhookId),
    queryFn: ({ signal }) =>
      api
        .get<{ deliveries: WebhookDelivery[] }>(`/api/app/webhooks/${webhookId}/deliveries`, { signal })
        .then((res) => res.deliveries),
  });

/** Sends a delivery again now, with its original body and webhook-id, to the webhook's current URL and signed with its current secret. The answer is the delivery after the attempt, which may have failed again. */
export const useResendWebhookDelivery = (workspaceId: string, webhookId: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (deliveryId: string) =>
      api.post<WebhookDelivery>(`/api/app/webhooks/${webhookId}/deliveries/${deliveryId}/resend`),
    onSuccess: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: webhookKeys.deliveries(workspaceId, webhookId) }),
        queryClient.invalidateQueries({ queryKey: webhookKeys.all(workspaceId), exact: true }),
      ]),
  });
};

/** Replaces a webhook's signing secret; the answer is the only time the new one is shown. */
export const useRegenerateWebhookSecret = (workspaceId: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (webhookId: string) =>
      api.post<{ secret: string }>(`/api/app/webhooks/${webhookId}/secret`).then((res) => res.secret),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: webhookKeys.all(workspaceId), exact: true }),
  });
};
