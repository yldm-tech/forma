import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { type ReactNode, Suspense } from "react";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../lib/i18n";
import type { WorkspaceDetail } from "../../../lib/queries/workspaces";
import { toast } from "../../../lib/toast";
import { IntegrationsPage } from "../../workspace-settings/integrations";
import type {
  Integration,
  IntegrationsOverview,
  LinkableSurvey,
  Webhook,
  WebhookDelivery,
  WebhookHealth,
} from "../api";
import { AirtablePage } from "./airtable";
import { GoogleSheetsPage } from "./google-sheets";
import { NotionPage } from "./notion";
import { SlackPage } from "./slack";
import { WebhooksPage } from "./webhooks";

const WS = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0f";
const BASE = `/api/app/workspaces/${WS}/integrations`;

let i18n: I18n;
const t = (key: string, options?: Record<string, unknown>) => i18n.t(key, options);

beforeAll(async () => {
  i18n = await initI18n("en-US");
  // jsdom has no modal dialogs; showing one is setting its open attribute.
  HTMLDialogElement.prototype.showModal = function showModal(this: HTMLDialogElement) {
    this.setAttribute("open", "");
  };
  HTMLDialogElement.prototype.close = function close(this: HTMLDialogElement) {
    this.removeAttribute("open");
  };
  // The router restores the scroll position on navigation, which jsdom does not implement.
  window.scrollTo = () => {};
  // Radix's checkbox measures itself; jsdom lays nothing out, so there is nothing to observe.
  globalThis.ResizeObserver ??= class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
});

afterEach(() => {
  cleanup();
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
  vi.unstubAllGlobals();
});

interface Call {
  method: string;
  path: string;
  body: unknown;
}

type Handler = (body: unknown) => { status?: number; body?: unknown };

const reply = (status: number, body?: unknown) =>
  status === 204
    ? new Response(null, { status })
    : new Response(JSON.stringify(body ?? {}), { status, headers: { "content-type": "application/json" } });

/** The Go server, answering "METHOD /path" (query string ignored) from handlers and recording every request. */
const stubServer = (handlers: Record<string, Handler>): Call[] => {
  const calls: Call[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string, init: RequestInit = {}) => {
      const method = init.method ?? "GET";
      const path = input.split("?")[0] ?? input;
      const body = typeof init.body === "string" ? (JSON.parse(init.body) as unknown) : undefined;
      calls.push({ method, path: input, body });
      const handler = handlers[`${method} ${path}`];
      if (!handler) return reply(404, { code: "not_found", message: `no stub for ${method} ${path}` });
      const answer = handler(body);
      return reply(answer.status ?? 200, answer.body);
    })
  );
  return calls;
};

const renderPage = (node: ReactNode) => {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const rootRoute = createRootRoute({ component: Outlet });
  const routeTree = rootRoute.addChildren([
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/",
      component: () => <Suspense fallback={null}>{node}</Suspense>,
    }),
  ]);
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: ["/"] }) });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
};

const toasts = () => toast.getSnapshot().map((shown) => shown.message);

const survey: LinkableSurvey = {
  id: "0192f7a8-1c3e-7b2a-9d4f-000000000001",
  name: "Feedback",
  elements: [
    { id: "q1", type: "openText", headline: { default: "What is your name?" } },
    { id: "q2", type: "rating", headline: { default: "How was it?" } },
  ],
  hiddenFields: [],
  variables: [],
};

const integration = <T extends Integration["type"]>(
  type: T,
  data: Integration<T>["config"]["data"],
  connection: Integration["connection"]
): Integration<T> => ({
  id: `integration-${type}`,
  type,
  config: { data },
  connection,
  createdAt: "2026-10-01T00:00:00.000Z",
  updatedAt: "2026-10-01T00:00:00.000Z",
  lastErrorAt: null,
  lastErrorMessage: null,
  consecutiveFailures: 0,
});

const overview = (integrations: Integration[], enabled = true): IntegrationsOverview => ({
  integrations,
  enabled: { googleSheets: enabled, airtable: enabled, notion: enabled, slack: enabled },
});

const webhook = (overrides: Partial<Webhook> = {}): Webhook => ({
  id: "0192f7a8-1c3e-7b2a-9d4f-0000000000a1",
  name: "Orders",
  createdAt: "2026-10-01T00:00:00.000Z",
  updatedAt: "2026-10-02T00:00:00.000Z",
  url: "https://hooks.example.com/forma",
  source: "user",
  workspaceId: WS,
  triggers: ["responseFinished"],
  surveyIds: [],
  allSurveys: true,
  ...overrides,
});

const failingHealth: WebhookHealth = {
  status: "failing",
  lastAttemptAt: "2026-10-26T00:00:00.000Z",
  lastSuccessAt: null,
  failingSince: "2026-10-25T00:00:00.000Z",
  nextAttemptAt: null,
  failedCount: 1,
  statusCode: 405,
  failure: "status",
  failureMessage: "Status 405",
};

const delivery = (overrides: Partial<WebhookDelivery> = {}): WebhookDelivery => ({
  id: "0192f7a8-1c3e-7b2a-9d4f-0000000000d1",
  createdAt: "2026-10-25T00:00:00.000Z",
  event: "responseFinished",
  responseId: "0192f7a8-1c3e-7b2a-9d4f-0000000000e1",
  surveyId: survey.id,
  surveyName: "Feedback",
  messageId: "3f5a",
  status: "failed",
  attempts: 8,
  lastAttemptAt: "2026-10-26T00:00:00.000Z",
  nextAttemptAt: null,
  latencyMs: 41,
  statusCode: 405,
  failure: "status",
  failureMessage: "Status 405",
  ...overrides,
});

describe("integrations page", () => {
  const workspace = (isReadOnly: boolean, appSetupCompleted = false) =>
    ({
      id: WS,
      appSetupCompleted,
      access: { role: "member", canWrite: !isReadOnly, canManage: false, isReadOnly },
    }) as unknown as WorkspaceDetail;

  const card = (label: string) => within(screen.getByRole("heading", { name: label }).parentElement!);

  test("counts webhooks by source and offers Manage for a connected integration", async () => {
    stubServer({
      [`GET ${BASE}`]: () => ({ body: overview([integration("googleSheets", [], { connected: true })]) }),
      [`GET /api/app/workspaces/${WS}/webhooks`]: () => ({
        body: {
          webhooks: [webhook(), webhook({ id: "w2" }), webhook({ id: "w3", source: "zapier" })],
          allowInternalUrls: false,
        },
      }),
    });
    renderPage(<IntegrationsPage workspace={workspace(false)} />);

    expect(await screen.findByText("2 webhooks")).toBeTruthy();
    expect(card("Zapier").getByText("1 zap")).toBeTruthy();
    const sheets = card("Google Sheets");
    expect(sheets.getByRole("link", { name: t("common.manage") }).getAttribute("href")).toBe(
      `/workspaces/${WS}/integrations/google-sheets`
    );
    expect(
      card("Slack")
        .getByRole("link", { name: t("common.connect") })
        .getAttribute("href")
    ).toBe(`/workspaces/${WS}/integrations/slack`);
    expect(card("Slack").getByText(t("common.not_connected"))).toBeTruthy();
  });

  test("a webhooks card turns red for a failing webhook and amber for one being retried or with lost deliveries to resend, and says how many", async () => {
    stubServer({
      [`GET ${BASE}`]: () => ({ body: overview([]) }),
      [`GET /api/app/workspaces/${WS}/webhooks`]: () => ({
        body: {
          webhooks: [
            webhook({ health: failingHealth }),
            webhook({ id: "w2", health: { ...failingHealth, status: "retrying", failedCount: 0 } }),
            webhook({ id: "w3" }),
            webhook({
              id: "w4",
              source: "zapier",
              health: { ...failingHealth, status: "retrying", failedCount: 0 },
            }),
            webhook({
              id: "w5",
              source: "make",
              health: { ...failingHealth, status: "healthy", failure: null },
            }),
            webhook({
              id: "w6",
              source: "n8n",
              health: { ...failingHealth, status: "lost", failure: null, failingSince: null },
            }),
          ],
          allowInternalUrls: false,
        },
      }),
    });
    renderPage(<IntegrationsPage workspace={workspace(false)} />);

    expect(await screen.findByText("3 webhooks · 1 failing · 1 retrying")).toBeTruthy();
    const dot = (label: string) =>
      screen.getByRole("heading", { name: label }).parentElement!.querySelector(".relative.inline-flex")!
        .className;
    expect(dot("Webhooks")).toContain("bg-red-500");
    expect(card("Zapier").getByText("1 zap · 1 retrying")).toBeTruthy();
    expect(dot("Zapier")).toContain("bg-amber-500");
    expect(card("Make.com").getByText("1 integration")).toBeTruthy();
    expect(dot("Make.com")).toContain("bg-green-500");
    expect(card("n8n").getByText("1 integration · 1 with lost deliveries")).toBeTruthy();
    expect(dot("n8n")).toContain("bg-amber-500");
  });

  test("an integration the instance cannot offer says so and only leads to its explanation; a set-up SDK is managed", async () => {
    stubServer({
      [`GET ${BASE}`]: () => ({ body: overview([], false) }),
      [`GET /api/app/workspaces/${WS}/webhooks`]: () => ({
        body: { webhooks: [], allowInternalUrls: false },
      }),
    });
    renderPage(<IntegrationsPage workspace={workspace(false, true)} />);

    await screen.findAllByText(t("workspace.integrations.not_available_on_this_instance"));
    for (const label of ["Google Sheets", "Airtable", "Slack", "Notion"]) {
      const integration = card(label);
      expect(integration.getByText(t("workspace.integrations.not_available_on_this_instance"))).toBeTruthy();
      expect(integration.queryByRole("link", { name: t("common.connect") })).toBeNull();
      expect(integration.getByRole("link", { name: t("common.learn_more") }).getAttribute("href")).toMatch(
        new RegExp(`^/workspaces/${WS}/integrations/`)
      );
    }
    const sdk = card("Javascript SDK");
    expect(sdk.getByText(t("common.connected"))).toBeTruthy();
    expect(sdk.getByRole("link", { name: t("common.manage") }).getAttribute("href")).toBe(
      `/workspaces/${WS}/integrations/app-connection`
    );
  });

  // The cards linked to zapier.com/apps/forma and activepieces.com/pieces/forma, which return 404, to a Make page for an app that was never published, and to n8n's home page.
  test("the automation platforms open a webhook guide in the app instead of linking to an app that does not exist", async () => {
    stubServer({
      [`GET ${BASE}`]: () => ({ body: overview([]) }),
      [`GET /api/app/workspaces/${WS}/webhooks`]: () => ({
        body: { webhooks: [], allowInternalUrls: false },
      }),
    });
    renderPage(<IntegrationsPage workspace={workspace(false)} />);

    await screen.findAllByText(t("common.not_connected"));
    const external = [...document.querySelectorAll("a")].filter(
      (a) => !a.getAttribute("href")?.startsWith("/")
    );
    expect(external.map((a) => a.getAttribute("href"))).toEqual([]);

    // The platform's own steps come before Forma's Add Webhook, which tests the URL and refuses one that answers 404: n8n's Production URL does until the workflow is active, and Activepieces' live URL only runs a published flow.
    const key = (name: string) => `workspace.integrations.automation_guide_${name}`;
    const steps = {
      Zapier: ["zapier_step_create", "zapier_step_copy"],
      "Make.com": ["make_step_create", "make_step_copy"],
      n8n: ["n8n_step_create", "n8n_step_copy", "n8n_step_activate"],
      Activepieces: ["activepieces_step_create", "activepieces_step_copy", "activepieces_step_publish"],
    };
    const docs = {
      Zapier: "https://help.zapier.com/",
      "Make.com": "https://apps.make.com/gateway",
      n8n: "https://docs.n8n.io/",
      Activepieces: "https://www.activepieces.com/pieces/webhook",
    };
    for (const [label, platformSteps] of Object.entries(steps)) {
      const platform = card(label);
      expect(platform.queryByRole("link")).toBeNull();
      fireEvent.click(
        platform.getByRole("button", { name: t("workspace.integrations.automation_guide_button") })
      );

      const dialog = screen.getByRole("dialog");
      const guide = within(dialog);
      const name = label === "Make.com" ? "Make" : label;
      const title = t("workspace.integrations.automation_guide_title", { platform: name });
      expect(guide.getByRole("heading", { name: title })).toBeTruthy();
      // The card keeps counting only webhooks a platform's own app subscribed, so it still reads Not Connected after the guide; the guide must not promise to connect it.
      expect(title).not.toMatch(/connect/i);
      // "a n8n", "a Activepieces": the description names the platform without an article.
      expect(dialog.textContent).not.toMatch(new RegExp(`\\ban? ${name}\\b`));
      const items = guide.getAllByRole("listitem").map((item) => item.textContent);
      expect(items).toEqual([
        ...platformSteps.map((step) => t(key(step))),
        t("workspace.integrations.automation_guide_step_add_webhook"),
        t("workspace.integrations.automation_guide_step_send_sample", { platform: name }),
      ]);
      const docsLink = guide.getByRole("link", {
        name: t("workspace.integrations.automation_guide_docs_link", { platform: name }),
      });
      expect(docsLink.getAttribute("href")?.startsWith(docs[label as keyof typeof docs])).toBe(true);
      expect(
        guide
          .getByRole("link", { name: t("workspace.integrations.automation_guide_open_webhooks") })
          .getAttribute("href")
      ).toBe(`/workspaces/${WS}/integrations/webhooks`);

      // The footer's Close; the corner's close button has the same name.
      fireEvent.click(guide.getAllByRole("button", { name: t("common.close") })[0]!);
      expect(screen.queryByRole("dialog")).toBeNull();
    }
  });

  test("a read-only member cannot open the integrations, but still sees the webhooks", async () => {
    stubServer({
      [`GET ${BASE}`]: () => ({ body: overview([]) }),
      [`GET /api/app/workspaces/${WS}/webhooks`]: () => ({
        body: { webhooks: [], allowInternalUrls: false },
      }),
    });
    renderPage(<IntegrationsPage workspace={workspace(true)} />);

    await screen.findAllByText(t("common.not_connected"));
    expect(card("Google Sheets").getByRole("button", { name: t("common.connect") })).toHaveProperty(
      "disabled",
      true
    );
    for (const label of ["Zapier", "Make.com", "n8n", "Activepieces"]) {
      expect(
        card(label).getByRole("button", { name: t("workspace.integrations.automation_guide_button") })
      ).toHaveProperty("disabled", true);
    }
    expect(
      card("Webhooks").getByRole("link", { name: t("workspace.integrations.manage_webhooks") })
    ).toBeTruthy();
  });
});

describe("Google Sheets", () => {
  test("a refused authorization is reported, and Connect sends the browser to the provider", async () => {
    const replace = vi.fn();
    vi.stubGlobal("location", { ...globalThis.location, replace });
    const calls = stubServer({
      [`GET ${BASE}`]: () => ({ body: overview([]) }),
      [`GET ${BASE}/surveys`]: () => ({ body: [] }),
      [`POST ${BASE}/googleSheets/connect`]: () => ({
        body: { authUrl: "https://accounts.google.com/o/oauth2/auth?state=s" },
      }),
    });
    renderPage(<GoogleSheetsPage workspaceId={WS} oauthError="access_denied" />);

    fireEvent.click(
      await screen.findByRole("button", {
        name: t("workspace.integrations.google_sheets.connect_with_google_sheets"),
      })
    );

    await waitFor(() =>
      expect(replace).toHaveBeenCalledWith("https://accounts.google.com/o/oauth2/auth?state=s")
    );
    expect(calls.some((call) => call.method === "POST" && call.path === `${BASE}/googleSheets/connect`)).toBe(
      true
    );
    expect(toasts()).toContain(t("workspace.integrations.connecting_integration_failed_please_try_again"));
  });

  test("an instance without Google credentials cannot connect and says it is not configured", async () => {
    stubServer({
      [`GET ${BASE}`]: () => ({ body: overview([], false) }),
      [`GET ${BASE}/surveys`]: () => ({ body: [] }),
    });
    renderPage(<GoogleSheetsPage workspaceId={WS} />);

    const connect = await screen.findByRole("button", {
      name: t("workspace.integrations.google_sheets.connect_with_google_sheets"),
    });
    expect(connect).toHaveProperty("disabled", true);
    expect(
      screen.getByText(t("workspace.integrations.google_sheets.google_sheet_integration_is_not_configured"), {
        exact: false,
      })
    ).toBeTruthy();
  });

  const connected = (links: Integration<"googleSheets">["config"]["data"] = []) => ({
    [`GET ${BASE}`]: () => ({
      body: overview([integration("googleSheets", links, { connected: true, email: "me@example.com" })]),
    }),
    [`GET ${BASE}/surveys`]: () => ({ body: [survey] }),
    [`GET ${BASE}/googleSheets/validate`]: () => ({ body: { connected: true } }),
  });

  const existing = {
    createdAt: "2026-10-01T00:00:00.000Z",
    surveyId: "other",
    surveyName: "Other survey",
    elementIds: ["x"],
    elements: "All questions",
    spreadsheetId: "old",
    spreadsheetName: "Old sheet",
    includeCreatedAt: true,
  };

  test("linking a sheet looks up its name and saves every link", async () => {
    const calls = stubServer({
      ...connected([existing]),
      [`GET ${BASE}/googleSheets/spreadsheets/abc123`]: () => ({ body: { name: "Responses" } }),
      [`PUT ${BASE}/googleSheets`]: () => ({ body: { id: "integration-googleSheets" } }),
    });
    renderPage(<GoogleSheetsPage workspaceId={WS} />);

    expect(
      await screen.findByText(t("workspace.integrations.connected_with_email", { email: "me@example.com" }))
    ).toBeTruthy();
    expect(screen.getByText("Old sheet")).toBeTruthy();
    fireEvent.click(
      screen.getByRole("button", { name: t("workspace.integrations.google_sheets.link_new_sheet") })
    );
    const dialog = within(await screen.findByRole("dialog"));
    fireEvent.change(dialog.getByLabelText(t("workspace.integrations.google_sheets.spreadsheet_url")), {
      target: { value: "https://docs.google.com/spreadsheets/d/abc123/edit#gid=0" },
    });
    fireEvent.change(dialog.getByLabelText(t("common.select_survey")), { target: { value: survey.id } });
    // Choosing a survey selects every question.
    expect(dialog.getByRole("checkbox", { name: "How was it?" }).getAttribute("aria-checked")).toBe("true");
    fireEvent.click(
      dialog.getByRole("button", { name: t("workspace.integrations.google_sheets.link_google_sheet") })
    );

    await waitFor(() => expect(calls.some((call) => call.method === "PUT")).toBe(true));
    const saved = calls.find((call) => call.method === "PUT")?.body as { data: Record<string, unknown>[] };
    expect(saved.data).toHaveLength(2);
    expect(saved.data[0]).toEqual(existing);
    expect(saved.data[1]).toMatchObject({
      surveyId: survey.id,
      surveyName: "Feedback",
      spreadsheetId: "abc123",
      spreadsheetName: "Responses",
      elementIds: ["q1", "q2"],
      elements: t("common.all_questions"),
      includeCreatedAt: true,
      includeVariables: false,
      includeHiddenFields: false,
      includeMetadata: false,
      includeContactAttributes: false,
    });
    await waitFor(() =>
      expect(toasts()).toContain(t("workspace.integrations.integration_added_successfully"))
    );
  });

  test("a spreadsheet the account cannot open is reported and nothing is saved", async () => {
    const calls = stubServer({
      ...connected(),
      [`GET ${BASE}/googleSheets/spreadsheets/abc123`]: () => ({
        status: 403,
        body: { code: "insufficient_permission", message: "insufficient_permission" },
      }),
    });
    renderPage(<GoogleSheetsPage workspaceId={WS} />);

    fireEvent.click(
      await screen.findByRole("button", { name: t("workspace.integrations.google_sheets.link_new_sheet") })
    );
    const dialog = within(await screen.findByRole("dialog"));
    fireEvent.change(dialog.getByLabelText(t("workspace.integrations.google_sheets.spreadsheet_url")), {
      target: { value: "https://docs.google.com/spreadsheets/d/abc123" },
    });
    fireEvent.change(dialog.getByLabelText(t("common.select_survey")), { target: { value: survey.id } });
    fireEvent.click(
      dialog.getByRole("button", { name: t("workspace.integrations.google_sheets.link_google_sheet") })
    );

    await waitFor(() =>
      expect(toasts()).toContain(t("workspace.integrations.google_sheets.spreadsheet_permission_error"))
    );
    expect(calls.some((call) => call.method === "PUT")).toBe(false);
  });

  test("a credential Google no longer refreshes offers reconnecting", async () => {
    stubServer({
      ...connected(),
      [`GET ${BASE}/googleSheets/validate`]: () => ({
        status: 400,
        body: { code: "invalid_grant", message: "invalid_grant" },
      }),
    });
    renderPage(<GoogleSheetsPage workspaceId={WS} />);

    expect(
      await screen.findByText(t("workspace.integrations.google_sheets.reconnect_button_description"))
    ).toBeTruthy();
  });
});

describe("Airtable", () => {
  test("choosing a base loads its tables, and the link names the table", async () => {
    const calls = stubServer({
      [`GET ${BASE}`]: () => ({
        body: overview([integration("airtable", [], { connected: true, email: "me@example.com" })]),
      }),
      [`GET ${BASE}/surveys`]: () => ({ body: [survey] }),
      [`GET ${BASE}/airtable/bases`]: () => ({ body: [{ id: "app1", name: "CRM" }] }),
      [`GET ${BASE}/airtable/bases/app1/tables`]: () => ({ body: [{ id: "tbl1", name: "Leads" }] }),
      [`PUT ${BASE}/airtable`]: () => ({ body: { id: "integration-airtable" } }),
    });
    renderPage(<AirtablePage workspaceId={WS} />);

    fireEvent.click(
      await screen.findByRole("button", { name: t("workspace.integrations.airtable.link_new_table") })
    );
    const dialog = within(await screen.findByRole("dialog"));
    fireEvent.change(await dialog.findByLabelText(t("workspace.integrations.airtable.airtable_base")), {
      target: { value: "app1" },
    });
    await dialog.findByRole("option", { name: "Leads" });
    fireEvent.change(dialog.getByLabelText(t("workspace.integrations.airtable.table_name")), {
      target: { value: "tbl1" },
    });
    fireEvent.change(dialog.getByLabelText(t("common.select_survey")), { target: { value: survey.id } });
    // Airtable starts with no question selected, as forma2's modal did.
    fireEvent.click(dialog.getByRole("checkbox", { name: "How was it?" }));
    fireEvent.click(dialog.getByRole("button", { name: t("common.save") }));

    await waitFor(() => expect(calls.some((call) => call.method === "PUT")).toBe(true));
    const saved = calls.find((call) => call.method === "PUT")?.body as { data: Record<string, unknown>[] };
    expect(saved.data).toEqual([
      expect.objectContaining({
        baseId: "app1",
        tableId: "tbl1",
        tableName: "Leads",
        elementIds: ["q2"],
        elements: t("common.selected_questions"),
      }),
    ]);
  });

  test("bases that cannot be listed offer reconnecting", async () => {
    stubServer({
      [`GET ${BASE}`]: () => ({
        body: overview([integration("airtable", [], { connected: true, email: "me@example.com" })]),
      }),
      [`GET ${BASE}/surveys`]: () => ({ body: [] }),
      [`GET ${BASE}/airtable/bases`]: () => ({
        status: 502,
        body: { code: "integration_provider_error", message: "Failed to get Airtable token" },
      }),
    });
    renderPage(<AirtablePage workspaceId={WS} />);

    expect(await screen.findByText(t("workspace.integrations.reconnect_button_description"))).toBeTruthy();
  });
});

describe("Notion", () => {
  test("a property an element cannot be written to is explained and blocks the link until it is fixed", async () => {
    const calls = stubServer({
      [`GET ${BASE}`]: () => ({
        body: overview([integration("notion", [], { connected: true, workspaceName: "Team space" })]),
      }),
      [`GET ${BASE}/surveys`]: () => ({ body: [survey] }),
      [`GET /api/app/workspaces/${WS}/contact-attribute-keys`]: () => ({ body: [] }),
      [`GET ${BASE}/notion/databases`]: () => ({
        body: [
          {
            id: "db1",
            name: "Responses",
            properties: [
              { id: "p1", name: "Name", type: "title" },
              { id: "p2", name: "Score", type: "number" },
            ],
          },
        ],
      }),
      [`PUT ${BASE}/notion`]: () => ({ body: { id: "integration-notion" } }),
    });
    renderPage(<NotionPage workspaceId={WS} />);

    expect(
      await screen.findByText(
        t("workspace.integrations.notion.connected_with_workspace", { workspace: "Team space" })
      )
    ).toBeTruthy();
    fireEvent.click(
      screen.getByRole("button", { name: t("workspace.integrations.notion.link_new_database") })
    );
    const dialog = within(await screen.findByRole("dialog"));
    fireEvent.change(await dialog.findByLabelText(t("workspace.integrations.notion.select_a_database")), {
      target: { value: "db1" },
    });
    fireEvent.change(dialog.getByLabelText(t("common.select_survey")), { target: { value: survey.id } });
    fireEvent.change(dialog.getByLabelText(t("workspace.integrations.notion.select_a_survey_question")), {
      target: { value: "q2" },
    });
    fireEvent.change(dialog.getByLabelText(t("workspace.integrations.notion.select_a_field_to_map")), {
      target: { value: "p1" },
    });

    expect(dialog.getByRole("alert").textContent).toContain("Mapping Error");
    const link = dialog.getByRole("button", { name: t("workspace.integrations.notion.link_database") });
    expect(link).toHaveProperty("disabled", true);

    fireEvent.change(dialog.getByLabelText(t("workspace.integrations.notion.select_a_field_to_map")), {
      target: { value: "p2" },
    });
    expect(dialog.queryByRole("alert")).toBeNull();
    fireEvent.click(link);

    await waitFor(() => expect(calls.some((call) => call.method === "PUT")).toBe(true));
    const saved = calls.find((call) => call.method === "PUT")?.body as { data: Record<string, unknown>[] };
    expect(saved.data).toEqual([
      {
        createdAt: expect.any(String),
        surveyId: survey.id,
        surveyName: "Feedback",
        databaseId: "db1",
        databaseName: "Responses",
        mapping: [
          {
            element: { id: "q2", name: "How was it?", type: "rating" },
            column: { id: "p2", name: "Score", type: "number" },
          },
        ],
      },
    ]);
  });
});

describe("Slack", () => {
  const slackLink = {
    createdAt: "2026-10-01T00:00:00.000Z",
    surveyId: survey.id,
    surveyName: "Feedback",
    elementIds: ["q1"],
    elements: "Selected questions",
    channelId: "C1",
    channelName: "general",
  };

  test("Slack refusing for a missing scope offers reconnecting", async () => {
    stubServer({
      [`GET ${BASE}`]: () => ({
        body: overview([integration("slack", [], { connected: true, teamName: "Acme" })]),
      }),
      [`GET ${BASE}/surveys`]: () => ({ body: [] }),
      [`GET ${BASE}/slack/channels`]: () => ({
        status: 502,
        body: { code: "integration_provider_error", message: "missing_scope" },
      }),
    });
    renderPage(<SlackPage workspaceId={WS} />);

    expect(
      await screen.findByRole("button", { name: t("workspace.integrations.slack.slack_reconnect_button") })
    ).toBeTruthy();
    expect(
      screen.getByText(t("workspace.integrations.slack.connected_with_team", { team: "Acme" }))
    ).toBeTruthy();
  });

  test("deleting a link saves the others", async () => {
    const calls = stubServer({
      [`GET ${BASE}`]: () => ({
        body: overview([integration("slack", [slackLink], { connected: true, teamName: "Acme" })]),
      }),
      [`GET ${BASE}/surveys`]: () => ({ body: [survey] }),
      [`GET ${BASE}/slack/channels`]: () => ({ body: [{ id: "C1", name: "general" }] }),
      [`PUT ${BASE}/slack`]: () => ({ body: { id: "integration-slack" } }),
    });
    renderPage(<SlackPage workspaceId={WS} />);

    fireEvent.click(await screen.findByRole("button", { name: /general/ }));
    const dialog = within(await screen.findByRole("dialog"));
    expect(dialog.getByLabelText(t("workspace.integrations.slack.select_channel"))).toHaveProperty(
      "value",
      "C1"
    );
    fireEvent.click(dialog.getByRole("button", { name: t("common.delete") }));

    await waitFor(() => expect(calls.some((call) => call.method === "PUT")).toBe(true));
    expect(calls.find((call) => call.method === "PUT")?.body).toEqual({ data: [] });
    await waitFor(() =>
      expect(toasts()).toContain(t("workspace.integrations.integration_removed_successfully"))
    );
  });
});

describe("webhooks", () => {
  const WEBHOOKS = `/api/app/workspaces/${WS}/webhooks`;
  const surveysList = () => ({
    body: {
      data: [{ id: survey.id, name: "Feedback" }],
      meta: { limit: 200, nextCursor: null, totalCount: 1, workspaceSurveyCount: 1 },
    },
  });

  test("adding a webhook tests the endpoint first and keeps the secret that signed the test, shown once", async () => {
    const calls = stubServer({
      [`GET ${WEBHOOKS}`]: () => ({ body: { webhooks: [], allowInternalUrls: false } }),
      "GET /api/v3/surveys": surveysList,
      [`POST ${WEBHOOKS}/test`]: () => ({ body: { success: true, secret: "whsec_c2VjcmV0" } }),
      [`POST ${WEBHOOKS}`]: (body) => ({
        status: 201,
        body: webhook({ ...(body as Partial<Webhook>), secret: "whsec_c2VjcmV0" }),
      }),
    });
    renderPage(<WebhooksPage workspaceId={WS} isReadOnly={false} />);

    expect(await screen.findByText(t("workspace.integrations.webhooks.empty_webhook_message"))).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: t("workspace.integrations.webhooks.add_webhook") }));
    const dialog = within(await screen.findByRole("dialog"));
    fireEvent.change(dialog.getByLabelText(t("common.url")), {
      target: { value: "https://hooks.example.com/forma" },
    });
    fireEvent.click(
      dialog.getByRole("checkbox", { name: t("workspace.integrations.webhooks.response_finished") })
    );
    fireEvent.click(
      dialog.getByRole("checkbox", { name: t("workspace.integrations.webhooks.all_current_and_new_surveys") })
    );
    fireEvent.click(dialog.getByRole("button", { name: t("workspace.integrations.webhooks.add_webhook") }));

    expect(await screen.findByDisplayValue("whsec_c2VjcmV0")).toBeTruthy();
    const posts = calls.filter((call) => call.method === "POST");
    expect(posts.map((call) => call.path)).toEqual([`${WEBHOOKS}/test`, WEBHOOKS]);
    expect(posts[0]?.body).toEqual({ url: "https://hooks.example.com/forma" });
    expect(posts[1]?.body).toEqual({
      name: "",
      url: "https://hooks.example.com/forma",
      source: "user",
      triggers: ["responseFinished"],
      surveyIds: [],
      allSurveys: true,
      webhookSecret: "whsec_c2VjcmV0",
    });
  });

  test("an endpoint the test could not reach is reported in the user's language and nothing is created", async () => {
    const calls = stubServer({
      [`GET ${WEBHOOKS}`]: () => ({ body: { webhooks: [], allowInternalUrls: false } }),
      "GET /api/v3/surveys": surveysList,
      [`POST ${WEBHOOKS}/test`]: () => ({
        status: 400,
        body: { code: "endpoint_not_found_error", message: "Not Found (404): The endpoint doesn't exist" },
      }),
    });
    vi.spyOn(console, "error").mockImplementation(() => {});
    renderPage(<WebhooksPage workspaceId={WS} isReadOnly={false} />);

    fireEvent.click(
      await screen.findByRole("button", { name: t("workspace.integrations.webhooks.add_webhook") })
    );
    const dialog = within(await screen.findByRole("dialog"));
    fireEvent.change(dialog.getByLabelText(t("common.url")), {
      target: { value: "https://hooks.example.com/forma" },
    });
    fireEvent.click(dialog.getByRole("button", { name: t("workspace.integrations.webhooks.test_endpoint") }));

    await waitFor(() =>
      expect(toasts()).toContain(
        `${t("workspace.integrations.webhooks.endpoint_pinged_error")} \n ${t("workspace.integrations.webhooks.endpoint_not_found_error")}`
      )
    );
    expect(calls.filter((call) => call.method === "POST").map((call) => call.path)).toEqual([
      `${WEBHOOKS}/test`,
    ]);
  });

  test("the form refuses an http URL before asking the server", async () => {
    const calls = stubServer({
      [`GET ${WEBHOOKS}`]: () => ({ body: { webhooks: [], allowInternalUrls: false } }),
      "GET /api/v3/surveys": surveysList,
    });
    renderPage(<WebhooksPage workspaceId={WS} isReadOnly={false} />);

    fireEvent.click(
      await screen.findByRole("button", { name: t("workspace.integrations.webhooks.add_webhook") })
    );
    const dialog = within(await screen.findByRole("dialog"));
    fireEvent.change(dialog.getByLabelText(t("common.url")), {
      target: { value: "http://hooks.example.com" },
    });
    fireEvent.click(dialog.getByRole("button", { name: t("workspace.integrations.webhooks.test_endpoint") }));

    await waitFor(() => expect(toasts()).toContain("URL must start with https://"));
    expect(calls.some((call) => call.method === "POST")).toBe(false);
  });

  test("a webhook on every survey says so, in the list and in its details, instead of listing today's surveys", async () => {
    stubServer({
      [`GET ${WEBHOOKS}`]: () => ({
        body: { webhooks: [webhook({ allSurveys: true })], allowInternalUrls: false },
      }),
      "GET /api/v3/surveys": surveysList,
    });
    renderPage(<WebhooksPage workspaceId={WS} isReadOnly={false} />);

    const row = await screen.findByRole("button", { name: /Orders/ });
    await waitFor(() =>
      expect(
        within(row).getByText(t("workspace.integrations.webhooks.all_current_and_new_surveys"))
      ).toBeTruthy()
    );
    expect(within(row).queryByText("Feedback")).toBeNull();
    expect(within(row).getByText(t("common.user"))).toBeTruthy();
    expect(within(row).getByTitle(webhook().url)).toBeTruthy();
    fireEvent.click(row);
    const dialog = within(await screen.findByRole("dialog"));
    expect(dialog.getByText(t("workspace.integrations.webhooks.all_current_and_new_surveys"))).toBeTruthy();
    expect(dialog.queryByText("Feedback")).toBeNull();
  });

  test("editing tests the endpoint with the stored secret, which the browser never sees", async () => {
    const existing = webhook({ surveyIds: [survey.id], allSurveys: false });
    const calls = stubServer({
      [`GET ${WEBHOOKS}`]: () => ({ body: { webhooks: [existing], allowInternalUrls: false } }),
      "GET /api/v3/surveys": surveysList,
      [`POST ${WEBHOOKS}/test`]: () => ({ body: { success: true } }),
      [`PATCH /api/app/webhooks/${existing.id}`]: (body) => ({ body: webhook(body as Partial<Webhook>) }),
    });
    renderPage(<WebhooksPage workspaceId={WS} isReadOnly={false} />);

    fireEvent.click(await screen.findByRole("button", { name: /Orders/ }));
    const dialog = within(await screen.findByRole("dialog"));
    fireEvent.click(dialog.getByRole("tab", { name: t("common.settings") }));
    fireEvent.change(dialog.getByLabelText(t("common.name")), { target: { value: "Orders v2" } });
    fireEvent.click(dialog.getByRole("button", { name: t("common.save_changes") }));

    await waitFor(() => expect(calls.some((call) => call.method === "PATCH")).toBe(true));
    expect(calls.find((call) => call.method === "POST")?.body).toEqual({
      url: existing.url,
      webhookId: existing.id,
    });
    expect(calls.find((call) => call.method === "PATCH")?.body).toEqual({
      name: "Orders v2",
      url: existing.url,
      source: "user",
      triggers: ["responseFinished"],
      surveyIds: [survey.id],
      allSurveys: false,
    });
  });

  test("a webhook whose surveys were all deleted is saved with its own scope, never widened to every survey", async () => {
    const existing = webhook({ surveyIds: ["0192f7a8-1c3e-7b2a-9d4f-00000000dead"], allSurveys: false });
    const calls = stubServer({
      [`GET ${WEBHOOKS}`]: () => ({ body: { webhooks: [existing], allowInternalUrls: false } }),
      "GET /api/v3/surveys": surveysList,
      [`POST ${WEBHOOKS}/test`]: () => ({ body: { success: true } }),
      [`PATCH /api/app/webhooks/${existing.id}`]: (body) => ({ body: webhook(body as Partial<Webhook>) }),
    });
    renderPage(<WebhooksPage workspaceId={WS} isReadOnly={false} />);

    fireEvent.click(await screen.findByRole("button", { name: /Orders/ }));
    const dialog = within(await screen.findByRole("dialog"));
    expect(dialog.queryByText("Feedback")).toBeNull();
    fireEvent.click(dialog.getByRole("tab", { name: t("common.settings") }));
    expect(
      dialog
        .getByRole("checkbox", { name: t("workspace.integrations.webhooks.all_current_and_new_surveys") })
        .getAttribute("aria-checked")
    ).toBe("false");
    fireEvent.change(dialog.getByLabelText(t("common.name")), { target: { value: "Orders v2" } });
    fireEvent.click(dialog.getByRole("button", { name: t("common.save_changes") }));

    await waitFor(() => expect(calls.some((call) => call.method === "PATCH")).toBe(true));
    expect(calls.find((call) => call.method === "PATCH")?.body).toMatchObject({
      surveyIds: [existing.surveyIds[0]],
      allSurveys: false,
    });
  });

  test("a webhook a third party created keeps its URL, triggers and surveys; a read-only member cannot add one", async () => {
    stubServer({
      [`GET ${WEBHOOKS}`]: () => ({
        body: { webhooks: [webhook({ source: "zapier", name: null })], allowInternalUrls: false },
      }),
      "GET /api/v3/surveys": surveysList,
    });
    renderPage(<WebhooksPage workspaceId={WS} isReadOnly />);

    fireEvent.click(await screen.findByRole("button", { name: /hooks\.example\.com/ }));
    expect(
      screen.queryByRole("button", { name: t("workspace.integrations.webhooks.add_webhook") })
    ).toBeNull();
    const dialog = within(await screen.findByRole("dialog"));
    // A platform is shown by its product name.
    expect(dialog.getByText("Zapier")).toBeTruthy();
    fireEvent.click(dialog.getByRole("tab", { name: t("common.settings") }));
    expect(dialog.getByLabelText(t("common.url"))).toHaveProperty("readOnly", true);
    expect(
      dialog.getByRole("checkbox", { name: t("workspace.integrations.webhooks.response_finished") })
    ).toHaveProperty("disabled", true);
    expect(dialog.queryByRole("button", { name: t("common.save_changes") })).toBeNull();
  });
  test("each webhook shows its delivery health, and its delivery log resends a lost delivery", async () => {
    const existing = webhook({ health: failingHealth });
    const calls = stubServer({
      [`GET ${WEBHOOKS}`]: () => ({ body: { webhooks: [existing], allowInternalUrls: false } }),
      "GET /api/v3/surveys": surveysList,
      [`GET /api/app/webhooks/${existing.id}/deliveries`]: () => ({ body: { deliveries: [delivery()] } }),
      [`POST /api/app/webhooks/${existing.id}/deliveries/${delivery().id}/resend`]: () => ({
        body: delivery({
          status: "succeeded",
          attempts: 9,
          statusCode: 200,
          failure: null,
          failureMessage: null,
        }),
      }),
    });
    renderPage(<WebhooksPage workspaceId={WS} isReadOnly={false} />);

    const row = await screen.findByRole("button", { name: /Orders/ });
    const badge = within(row).getByText(t("workspace.integrations.webhooks.deliveries.health_failing"));
    expect(badge.getAttribute("title")).toContain("The endpoint answered with status 405");
    fireEvent.click(row);
    const modal = within(await screen.findByRole("dialog"));
    expect(modal.getByText("The endpoint answered with status 405")).toBeTruthy();
    fireEvent.click(
      modal.getByRole("button", {
        name: t("workspace.integrations.webhooks.deliveries.view_recent_deliveries"),
      })
    );

    const sheet = within(
      await screen.findByRole("dialog", {
        name: t("workspace.integrations.webhooks.deliveries.recent_deliveries"),
      })
    );
    expect(
      await sheet.findByText(t("workspace.integrations.webhooks.deliveries.status_failed"))
    ).toBeTruthy();
    expect(sheet.getByRole("link", { name: "Feedback" }).getAttribute("href")).toBe(
      `/workspaces/${WS}/surveys/${survey.id}/responses`
    );
    fireEvent.click(
      sheet.getByRole("button", { name: t("workspace.integrations.webhooks.deliveries.resend") })
    );

    await waitFor(() =>
      expect(toasts()).toContain(t("workspace.integrations.webhooks.deliveries.resend_succeeded"))
    );
    expect(calls.filter((call) => call.method === "POST").map((call) => call.path)).toEqual([
      `/api/app/webhooks/${existing.id}/deliveries/${delivery().id}/resend`,
    ]);
  });

  test("a resend that fails says why, even of an event delivered before; a read-only member sees the log without Resend", async () => {
    const existing = webhook({ health: failingHealth });
    stubServer({
      [`GET ${WEBHOOKS}`]: () => ({ body: { webhooks: [existing], allowInternalUrls: false } }),
      "GET /api/v3/surveys": surveysList,
      [`GET /api/app/webhooks/${existing.id}/deliveries`]: () => ({ body: { deliveries: [delivery()] } }),
      [`POST /api/app/webhooks/${existing.id}/deliveries/${delivery().id}/resend`]: () => ({
        // The event stays delivered; the failure is the resend's.
        body: delivery({
          status: "succeeded",
          attempts: 9,
          statusCode: null,
          failure: "timeout",
          failureMessage: "No answer",
        }),
      }),
    });
    renderPage(<WebhooksPage workspaceId={WS} isReadOnly={false} />);

    fireEvent.click(await screen.findByRole("button", { name: /Orders/ }));
    fireEvent.click(
      within(await screen.findByRole("dialog")).getByRole("button", {
        name: t("workspace.integrations.webhooks.deliveries.view_recent_deliveries"),
      })
    );
    const sheet = within(
      await screen.findByRole("dialog", {
        name: t("workspace.integrations.webhooks.deliveries.recent_deliveries"),
      })
    );
    fireEvent.click(
      await sheet.findByRole("button", { name: t("workspace.integrations.webhooks.deliveries.resend") })
    );
    await waitFor(() =>
      expect(toasts()).toContain(
        t("workspace.integrations.webhooks.deliveries.resend_failed", {
          reason: "No answer within 5 seconds",
        })
      )
    );
    cleanup();

    renderPage(<WebhooksPage workspaceId={WS} isReadOnly />);
    fireEvent.click(await screen.findByRole("button", { name: /Orders/ }));
    fireEvent.click(
      within(await screen.findByRole("dialog")).getByRole("button", {
        name: t("workspace.integrations.webhooks.deliveries.view_recent_deliveries"),
      })
    );
    const readOnly = within(
      await screen.findByRole("dialog", {
        name: t("workspace.integrations.webhooks.deliveries.recent_deliveries"),
      })
    );
    expect(
      await readOnly.findByText(t("workspace.integrations.webhooks.deliveries.status_failed"))
    ).toBeTruthy();
    expect(
      readOnly.queryByRole("button", { name: t("workspace.integrations.webhooks.deliveries.resend") })
    ).toBeNull();
  });

  test("regenerating the signing secret asks first and shows the new secret once; a platform's webhook keeps its secret", async () => {
    const existing = webhook();
    const calls = stubServer({
      [`GET ${WEBHOOKS}`]: () => ({
        body: {
          webhooks: [existing, webhook({ id: "w2", name: "Zap", source: "zapier" })],
          allowInternalUrls: false,
        },
      }),
      "GET /api/v3/surveys": surveysList,
      [`POST /api/app/webhooks/${existing.id}/secret`]: () => ({ body: { secret: "whsec_bmV3c2VjcmV0" } }),
    });
    renderPage(<WebhooksPage workspaceId={WS} isReadOnly={false} />);

    fireEvent.click(await screen.findByRole("button", { name: /Orders/ }));
    const dialog = within(await screen.findByRole("dialog"));
    fireEvent.click(dialog.getByRole("tab", { name: t("common.settings") }));
    fireEvent.click(
      dialog.getByRole("button", { name: t("workspace.integrations.webhooks.regenerate_secret.button") })
    );
    const confirm = within(
      await screen.findByRole("dialog", {
        name: t("workspace.integrations.webhooks.regenerate_secret.title"),
      })
    );
    expect(calls.some((call) => call.method === "POST")).toBe(false);
    fireEvent.click(
      confirm.getByRole("button", { name: t("workspace.integrations.webhooks.regenerate_secret.confirm") })
    );
    expect(await screen.findByDisplayValue("whsec_bmV3c2VjcmV0")).toBeTruthy();
    expect(calls.filter((call) => call.method === "POST").map((call) => call.path)).toEqual([
      `/api/app/webhooks/${existing.id}/secret`,
    ]);
    cleanup();

    renderPage(<WebhooksPage workspaceId={WS} isReadOnly={false} />);
    fireEvent.click(await screen.findByRole("button", { name: /Zap/ }));
    const zap = within(await screen.findByRole("dialog"));
    fireEvent.click(zap.getByRole("tab", { name: t("common.settings") }));
    expect(
      zap.getByText(
        t("workspace.integrations.webhooks.regenerate_secret.third_party", { platform: "Zapier" })
      )
    ).toBeTruthy();
    expect(
      zap.queryByRole("button", { name: t("workspace.integrations.webhooks.regenerate_secret.button") })
    ).toBeNull();
  });
});
