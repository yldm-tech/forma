import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import { ConnectAppBanner, permissionLabel } from "./api-keys";

// forma2's API keys page (modules/organization/settings/api-keys/page.tsx): a website or app survey needs no key, so the page points to the first workspace's SDK page while there is a workspace.

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
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
