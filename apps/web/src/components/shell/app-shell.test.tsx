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
import { PHONE_READY, useIsPhoneReadyPage } from "../../lib/phone";
import { AppShell } from "./app-shell";
import { plainSessionQuery } from "./plain-chat";

// The shell on a phone: the sidebar gives way to the top bar's drawer, and a page that opens there (PHONE_READY) says it is read-only.

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const stubScreen = (isPhone: boolean) =>
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: isPhone && query === "(max-width: 639px)",
    addEventListener: () => {},
    removeEventListener: () => {},
  }));

const ReadyProbe = () => <p>{useIsPhoneReadyPage() ? "phone-ready" : "desktop-only"}</p>;

/** The shell around a summary page (PHONE_READY), its nested response modal route (not marked) and an editor page (not marked). */
const renderShell = async (path: string) => {
  const queryClient = new QueryClient();
  queryClient.setQueryData(plainSessionQuery.queryKey, null);
  const shell = () => (
    <AppShell sidebar={<nav aria-label="Sidebar">sidebar</nav>} topBar={<div>top bar</div>}>
      <Outlet />
    </AppShell>
  );
  const rootRoute = createRootRoute({ component: shell });
  const summary = createRoute({
    getParentRoute: () => rootRoute,
    path: "/summary",
    staticData: PHONE_READY,
    component: () => (
      <>
        <ReadyProbe />
        <Outlet />
      </>
    ),
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      summary.addChildren([
        createRoute({ getParentRoute: () => summary, path: "/detail", component: () => null }),
      ]),
      createRoute({ getParentRoute: () => rootRoute, path: "/edit", component: ReadyProbe }),
    ]),
    history: createMemoryHistory({ initialEntries: [path] }),
  });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
  await waitFor(() => expect(router.state.status).toBe("idle"));
};

const notice = () => screen.queryByText(i18n.t("common.mobile_view_only_notice"));

test("on a phone a page that opens there says it is read-only", async () => {
  stubScreen(true);
  await renderShell("/summary");
  expect(screen.getByText("phone-ready")).toBeTruthy();
  expect(notice()).toBeTruthy();
});

test("the deepest page decides: a child the phone does not open is not read-only there", async () => {
  stubScreen(true);
  await renderShell("/summary/detail");
  expect(screen.getByText("desktop-only")).toBeTruthy();
  expect(notice()).toBeNull();
});

test("a page that does not open on a phone has no notice (the overlay covers it)", async () => {
  stubScreen(true);
  await renderShell("/edit");
  expect(screen.getByText("desktop-only")).toBeTruthy();
  expect(notice()).toBeNull();
});

test("from the sm breakpoint up there is no notice, and the sidebar is shown only from there", async () => {
  stubScreen(false);
  await renderShell("/summary");
  expect(notice()).toBeNull();
  expect(screen.getByRole("navigation", { name: "Sidebar" }).parentElement?.className).toBe("hidden sm:flex");
});
