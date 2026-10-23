import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { TFunction, i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import { CommandPalette, navigationDestinations } from "./command-palette";

// forma2's command-palette.tsx and navigation/lib/destinations.test.ts.

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
  // jsdom has no modal dialogs; showing one is setting its open attribute.
  HTMLDialogElement.prototype.showModal = function showModal(this: HTMLDialogElement) {
    this.setAttribute("open", "");
  };
  HTMLDialogElement.prototype.close = function close(this: HTMLDialogElement) {
    this.removeAttribute("open");
  };
  // cmdk scrolls the selected item into view and measures its list; jsdom lays nothing out.
  Element.prototype.scrollIntoView ??= () => {};
  globalThis.ResizeObserver ??= class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("navigationDestinations", () => {
  const t = ((key: string) => key) as unknown as TFunction;
  const build = (overrides: Partial<Parameters<typeof navigationDestinations>[1]> = {}) =>
    navigationDestinations(t, {
      workspaceId: "ws_1",
      organizationId: "org_1",
      isBilling: false,
      isOwnerOrManager: true,
      isFormaCloud: false,
      ...overrides,
    });
  const ids = (destinations: ReturnType<typeof build>) => destinations.map((d) => d.id);

  test("offers every product area and settings page a manager can open", () => {
    expect(ids(build())).toEqual([
      "surveys",
      "contacts",
      "workflows",
      "user-actions",
      "integrations",
      "app-connection",
      "tags",
      "ws-general",
      "ws-look",
      "ws-languages",
      "ws-teams",
      "org-general",
      "org-teams",
      "org-api-keys",
      "account-profile",
      "account-notifications",
    ]);
  });

  test("keeps the product areas out of the settings paths and the SDK inside integrations", () => {
    const result = build();
    for (const id of ["user-actions", "integrations", "tags"])
      expect(result.find((d) => d.id === id)?.href).toBe(`/workspaces/ws_1/${id}`);
    expect(result.find((d) => d.id === "app-connection")?.href).toBe(
      "/workspaces/ws_1/integrations/app-connection"
    );
  });

  test("offers the billing role only what it can open", () => {
    expect(ids(build({ isBilling: true }))).toEqual([
      "org-general",
      "org-teams",
      "org-api-keys",
      "account-profile",
    ]);
  });

  test("withholds API keys from a member", () => {
    expect(ids(build({ isOwnerOrManager: false }))).not.toContain("org-api-keys");
  });

  test("offers billing on Forma Cloud only", () => {
    expect(build({ isFormaCloud: true }).find((d) => d.id === "org-billing")?.href).toBe(
      "/organizations/org_1/settings/billing"
    );
    expect(ids(build())).not.toContain("org-billing");
  });
});

/** GET /api/v3/surveys answering one survey per name filter; the filters asked are recorded. */
const stubSurveys = () => {
  const asked: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname !== "/api/v3/surveys") throw new Error(`unexpected ${path}`);
      const name = url.searchParams.get("filter[name][contains]") ?? "";
      asked.push(name);
      return new Response(
        JSON.stringify({
          data: [{ id: "s1", name: `${name} survey`, workspaceId: "w1" }],
          meta: { limit: 5, nextCursor: null, totalCount: null, workspaceSurveyCount: null },
        }),
        { headers: { "content-type": "application/json" } }
      );
    })
  );
  return asked;
};

const renderPalette = async (isBilling = false) => {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const rootRoute = createRootRoute({ component: Outlet });
  const page = (path: string) =>
    createRoute({ getParentRoute: () => rootRoute, path, component: () => null });
  const routeTree = rootRoute.addChildren([
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/",
      component: () => (
        <CommandPalette
          workspaceId="w1"
          organizationId="o1"
          isBilling={isBilling}
          isOwnerOrManager
          isFormaCloud={false}
        />
      ),
    }),
    page("/workspaces/w1/integrations/app-connection"),
    page("/workspaces/w1/surveys/s1/summary"),
  ]);
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: ["/"] }) });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
  await router.load();
  return router;
};

const searchBox = () => screen.getByPlaceholderText(i18n.t("common.search"));

test("Cmd+K and Ctrl+K open and close the palette", async () => {
  await renderPalette();
  await screen.findByRole("button", { name: i18n.t("common.search") });
  expect(screen.queryByPlaceholderText(i18n.t("common.search"))).toBeNull();
  fireEvent.keyDown(document, { key: "k", metaKey: true });
  expect(await screen.findByPlaceholderText(i18n.t("common.search"))).toBeTruthy();
  fireEvent.keyDown(document, { key: "k", ctrlKey: true });
  await waitFor(() => expect(screen.queryByPlaceholderText(i18n.t("common.search"))).toBeNull());
});

test("typing filters the destinations, and choosing one navigates there", async () => {
  stubSurveys();
  const router = await renderPalette();
  fireEvent.keyDown(document, { key: "k", metaKey: true });
  fireEvent.change(await screen.findByPlaceholderText(i18n.t("common.search")), {
    target: { value: "snippet" },
  });
  await waitFor(() => expect(screen.queryByRole("option", { name: i18n.t("common.contacts") })).toBeNull());
  fireEvent.click(screen.getByRole("option", { name: i18n.t("common.web_and_mobile_sdk") }));
  await waitFor(() =>
    expect(router.state.location.pathname).toBe("/workspaces/w1/integrations/app-connection")
  );
  expect(screen.queryByPlaceholderText(i18n.t("common.search"))).toBeNull();
});

test("finds a survey by name and opens its summary", async () => {
  const asked = stubSurveys();
  const router = await renderPalette();
  fireEvent.keyDown(document, { key: "k", metaKey: true });
  fireEvent.change(await screen.findByPlaceholderText(i18n.t("common.search")), { target: { value: "NPS" } });
  fireEvent.click(await screen.findByRole("option", { name: "NPS survey" }));
  await waitFor(() => expect(router.state.location.pathname).toBe("/workspaces/w1/surveys/s1/summary"));
  expect(asked).toEqual(["NPS"]);
});

test("never looks surveys up for the billing role", async () => {
  const asked = stubSurveys();
  await renderPalette(true);
  fireEvent.keyDown(document, { key: "k", metaKey: true });
  fireEvent.change(await screen.findByPlaceholderText(i18n.t("common.search")), { target: { value: "NPS" } });
  await new Promise((resolve) => setTimeout(resolve, 400));
  expect(asked).toEqual([]);
  expect(searchBox()).toBeTruthy();
});
