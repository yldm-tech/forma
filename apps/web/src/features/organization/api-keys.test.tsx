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
import type { ReactNode } from "react";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import type { ApiKey, CreatedApiKey } from "../../lib/queries/api-keys";
import type { OrganizationDetail } from "../../lib/queries/organizations";
import { toast } from "../../lib/toast";
import { ApiKeysPage, ConnectAppBanner, permissionLabel } from "./api-keys";

// forma2's API keys page (modules/organization/settings/api-keys/page.tsx): a website or app survey needs no key, so the page points to the first workspace's SDK page while there is a workspace.

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
  HTMLDialogElement.prototype.showModal = function showModal(this: HTMLDialogElement) {
    this.setAttribute("open", "");
  };
  HTMLDialogElement.prototype.close = function close(this: HTMLDialogElement) {
    this.removeAttribute("open");
  };
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
});

const renderBanner = async (workspaces: { id: string; name: string }[]) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      if (path !== "/api/app/organizations/o1/workspaces") throw new Error(`unexpected ${path}`);
      return new Response(JSON.stringify({ workspaces }), {
        headers: { "content-type": "application/json" },
      });
    })
  );
  const rootRoute = createRootRoute({ component: Outlet });
  const routeTree = rootRoute.addChildren([
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/",
      component: () => <ConnectAppBanner organizationId="o1" />,
    }),
  ]);
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: ["/"] }) });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
  await router.load();
};

test("points to the first workspace's SDK page", async () => {
  await renderBanner([
    { id: "w1", name: "Website" },
    { id: "w2", name: "App" },
  ]);
  expect(
    await screen.findByText(i18n.t("workspace.settings.api_keys.connect_app_banner_title"))
  ).toBeTruthy();
  expect(
    screen
      .getByRole("link", { name: i18n.t("workspace.settings.api_keys.connect_app_banner_link") })
      .getAttribute("href")
  ).toBe("/workspaces/w1/integrations/app-connection");
});

test("is absent while the organization has no workspace", async () => {
  await renderBanner([]);
  await waitFor(() => expect(vi.mocked(fetch)).toHaveBeenCalled());
  expect(screen.queryByText(i18n.t("workspace.settings.api_keys.connect_app_banner_title"))).toBeNull();
});

test("names a workspace permission in the user's language, never by its enum value", async () => {
  expect((["read", "write", "manage"] as const).map((p) => permissionLabel(i18n.t, p))).toEqual([
    "Read",
    "Read & Write",
    "Manage",
  ]);
  const ja = await initI18n("ja-JP");
  const labels = (["read", "write", "manage"] as const).map((p) => permissionLabel(ja.t, p));
  expect(labels).toEqual([
    ja.t("workspace.settings.teams.read"),
    ja.t("workspace.settings.teams.read_write"),
    ja.t("workspace.settings.teams.manage"),
  ]);
  for (const label of labels) expect(label).not.toMatch(/^(read|write|manage)$/i);
});

const ORG = "0190a000-0000-7000-8000-000000000001";
const API = `/api/app/organizations/${ORG}`;

const owner: OrganizationDetail = {
  id: ORG,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
  name: "Acme",
  isAISmartToolsEnabled: false,
  displayTimeZone: null,
  surveySchedulingTimeZone: null,
  surveyScheduling: { timeZone: "UTC", localHour: 0, localMinute: 0, instanceTimeZone: "UTC" },
  whitelabel: {},
  membership: { organizationId: ORG, userId: "u1", accepted: true, role: "owner" },
  userManagementAllowed: true,
};

const existing: ApiKey = {
  id: "k1",
  label: "Existing",
  createdAt: "2026-01-01T00:00:00Z",
  organizationAccess: { accessControl: { read: true, write: false } },
  workspacePermissions: [],
};

/** The API keys endpoints over an in-memory list, so a create or delete shows up in the next list read. */
const stubKeysServer = () => {
  const keys: ApiKey[] = [existing];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      const key = `${init.method ?? "GET"} ${path}`;
      const json = (body: unknown) =>
        new Response(JSON.stringify(body), { headers: { "content-type": "application/json" } });
      if (key === `GET ${API}/workspaces`) return json({ workspaces: [] });
      if (key === `GET ${API}/api-keys`) return json(keys);
      if (key === `POST ${API}/api-keys`) {
        const created: CreatedApiKey = {
          id: "k2",
          label: "CI",
          createdAt: "2026-01-02T00:00:00Z",
          organizationAccess: { accessControl: { read: true, write: false } },
          workspacePermissions: [],
          key: "fbk_plaintext",
        };
        keys.push(created);
        return json(created);
      }
      if (key === `DELETE ${API}/api-keys/k2`) {
        keys.splice(
          keys.findIndex((k) => k.id === "k2"),
          1
        );
        return new Response(null, { status: 204 });
      }
      throw new Error(`unexpected ${key}`);
    })
  );
};

const renderPage = async (node: ReactNode) => {
  const rootRoute = createRootRoute({ component: Outlet });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({ getParentRoute: () => rootRoute, path: "/", component: () => node }),
    ]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
  await router.load();
};

test("tells the creator of a key that v3 and MCP can lag, only while that key is listed", async () => {
  stubKeysServer();
  await renderPage(<ApiKeysPage organization={owner} />);
  const note = i18n.t("workspace.api_keys.v3_propagation_note");
  await screen.findByText("Existing");
  expect(screen.queryByText(note)).toBeNull();

  fireEvent.click(screen.getByRole("button", { name: i18n.t("workspace.settings.api_keys.add_api_key") }));
  const dialog = await screen.findByRole("dialog");
  fireEvent.change(within(dialog).getByLabelText(i18n.t("workspace.api_keys.api_key_label")), {
    target: { value: "CI" },
  });
  fireEvent.click(
    within(dialog).getByRole("switch", { name: i18n.t("workspace.api_keys.organization_read") })
  );
  fireEvent.click(within(dialog).getByRole("button", { name: i18n.t("workspace.api_keys.add_api_key") }));

  expect((await screen.findByText(note)).getAttribute("role")).toBe("status");
  expect(await screen.findByText("fbk_plaintext")).toBeTruthy();

  // Deleting the key just created takes the note with it: no listed key is new any more.
  const row = screen.getByText("CI").closest("tr");
  if (!row) throw new Error("the new key has no row");
  fireEvent.click(within(row).getByRole("button", { name: i18n.t("common.delete") }));
  const confirm = await screen.findByRole("alertdialog");
  fireEvent.click(within(confirm).getByRole("button", { name: i18n.t("common.delete") }));
  await waitFor(() => expect(screen.queryByText("CI")).toBeNull());
  expect(screen.queryByText(note)).toBeNull();
  expect(screen.getByText("Existing")).toBeTruthy();
});
