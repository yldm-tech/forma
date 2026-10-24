import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { Suspense } from "react";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import type { UnsubscribeLink } from "../../lib/notifications";
import type { NotificationOrganization } from "../../lib/queries/auth";
import { forgetWorkspace, rememberWorkspace } from "../../lib/storage";
import { toast } from "../../lib/toast";
import { NotificationsPage } from "./notifications";

const WS = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0f";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
  window.scrollTo = () => {};
});

afterEach(() => {
  cleanup();
  forgetWorkspace();
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
  vi.unstubAllGlobals();
});

const organizations: NotificationOrganization[] = [
  {
    id: "o1",
    name: "Acme",
    autoSubscribe: true,
    surveys: [
      { id: "s1", name: "NPS Survey", workspaceId: "w2", workspaceName: "Website", alert: true },
      { id: "s2", name: "Onboarding", workspaceId: "w1", workspaceName: "Docs Portal", alert: false },
      { id: "s3", name: "Churn Survey", workspaceId: "w2", workspaceName: "Website", alert: false },
    ],
  },
];

/** The Go server: the notification settings, a PATCH that answers 204, and the remembered workspace. */
const stubServer = () => {
  const patches: unknown[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      const json = (body: unknown) =>
        new Response(JSON.stringify(body), { status: 200, headers: { "content-type": "application/json" } });
      if (path === "/api/app/auth/me/notifications" && init.method === "PATCH") {
        patches.push(JSON.parse(init.body as string));
        return new Response(null, { status: 204 });
      }
      if (path === "/api/app/auth/me/notifications") return json({ organizations });
      if (path === `/api/app/workspaces/${WS}`) return json({ id: WS, name: "Website" });
      return new Response(JSON.stringify({ code: "not_found", message: path }), {
        status: 404,
        headers: { "content-type": "application/json" },
      });
    })
  );
  return patches;
};

const renderPage = (link: UnsubscribeLink) => {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const rootRoute = createRootRoute({ component: Outlet });
  const routeTree = rootRoute.addChildren([
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/",
      component: () => (
        <Suspense fallback={null}>
          <NotificationsPage link={link} />
        </Suspense>
      ),
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

test("an email's unsubscribe link switches the survey's alert off once", async () => {
  const patches = stubServer();
  renderPage({ type: "alert", elementId: "s1" });

  await waitFor(() => expect(patches).toEqual([{ type: "alert", id: "s1", enabled: false }]));
  await waitFor(() =>
    expect(toast.getSnapshot().map((shown) => shown.message)).toContain(
      i18n.t(
        "workspace.settings.notifications.you_will_not_receive_any_more_emails_for_responses_on_this_survey"
      )
    )
  );
});

test("an unsubscribe link for an organization stops auto-subscribing to its surveys", async () => {
  const patches = stubServer();
  renderPage({ type: "unsubscribedOrganizationIds", elementId: "o1" });

  await waitFor(() =>
    expect(patches).toEqual([{ type: "unsubscribedOrganizationIds", id: "o1", enabled: false }])
  );
});

test("surveys are listed by workspace, then by name", async () => {
  stubServer();
  renderPage({});

  const table = await screen.findByRole("table", { name: "Acme" });
  const rows = within(table)
    .getAllByRole("row")
    .slice(1)
    .map((row) =>
      within(row)
        .getAllByRole("cell")
        .slice(0, 2)
        .map((cell) => cell.textContent)
    );
  expect(rows).toEqual([
    ["Onboarding", "Docs Portal"],
    ["Churn Survey", "Website"],
    ["NPS Survey", "Website"],
  ]);
});

test("Slack and Discord notifications point to the integrations of the workspace the user works in", async () => {
  stubServer();
  rememberWorkspace(WS);
  renderPage({});

  const link = await screen.findByRole("link", {
    name: i18n.t("workspace.settings.notifications.use_the_integration"),
  });
  expect(link.getAttribute("href")).toBe(`/workspaces/${WS}/integrations`);
});
