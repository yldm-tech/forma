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
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import type { Me } from "../../lib/queries/auth";
import { billingKeys } from "../../lib/queries/billing";
import { type OrganizationSummary, organizationsQuery } from "../../lib/queries/organizations";
import { type Workspace, workspacesQuery } from "../../lib/queries/workspaces";
import { TopControlBar } from "./top-control-bar";

const ORG = "0190a000-0000-7000-8000-000000000001";
const WS = "0190a000-0000-7000-8000-000000000002";

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
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const user: Me = {
  id: "u1",
  name: "Ada Owner",
  email: "ada@example.com",
  emailVerified: true,
  locale: "en-US",
  identityProvider: "email",
  hasPassword: true,
  twoFactorEnabled: false,
  createdAt: "2026-01-01T00:00:00Z",
  lastLoginAt: null,
};

/** The screen as phone-sized (below sm) or wider. */
const stubScreen = (isPhone: boolean) =>
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: isPhone && query === "(max-width: 639px)",
    addEventListener: () => {},
    removeEventListener: () => {},
  }));

const OTHER_ORG = "0190a000-0000-7000-8000-000000000003";

/** The workspace's top bar on its survey list, with the contacts page to navigate to. The switchers' lists are loaded already: the organization and another one, and the workspace. */
const renderTopBar = async (role = "owner") => {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  queryClient.setQueryData(billingKeys.summary(ORG), { isFormaCloud: false });
  queryClient.setQueryData(organizationsQuery.queryKey, [
    { id: ORG, name: "Acme", role, suspendedAt: null },
    { id: OTHER_ORG, name: "Globex", role: "member", suspendedAt: null },
  ] as OrganizationSummary[]);
  queryClient.setQueryData(workspacesQuery(ORG).queryKey, [{ id: WS, name: "Website" }] as Workspace[]);
  const page = () => (
    <TopControlBar
      user={user}
      organization={{ id: ORG, name: "Acme" }}
      workspace={{ id: WS, name: "Website" }}
      membershipRole={role}
    />
  );
  const rootRoute = createRootRoute({ component: Outlet });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({
        getParentRoute: () => rootRoute,
        path: "/workspaces/$workspaceId/surveys",
        component: page,
      }),
      createRoute({
        getParentRoute: () => rootRoute,
        path: "/workspaces/$workspaceId/contacts",
        component: page,
      }),
    ]),
    history: createMemoryHistory({ initialEntries: [`/workspaces/${WS}/surveys`] }),
  });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
  await waitFor(() => expect(router.state.status).toBe("idle"));
  return router;
};

const openDrawer = async () => {
  fireEvent.click(await screen.findByRole("button", { name: i18n.t("common.mobile_open_navigation") }));
  return screen.getByRole("dialog", { name: i18n.t("common.mobile_navigation_title") });
};

test("on a phone the menu opens the workspace's navigation, limited to the pages a phone opens", async () => {
  stubScreen(true);
  await renderTopBar();
  const menu = await screen.findByRole("button", { name: i18n.t("common.mobile_open_navigation") });
  expect(menu.getAttribute("aria-expanded")).toBe("false");
  const drawer = await openDrawer();
  expect(menu.getAttribute("aria-expanded")).toBe("true");
  const nav = within(drawer).getByRole("navigation", { name: i18n.t("common.workspace") });
  expect(
    within(nav)
      .getAllByRole("link")
      .map((link) => [link.textContent, link.getAttribute("href")])
  ).toEqual([
    [i18n.t("common.surveys"), `/workspaces/${WS}`],
    [i18n.t("common.contacts"), `/workspaces/${WS}/contacts`],
  ]);
  expect(
    within(nav)
      .getByRole("link", { name: i18n.t("common.surveys") })
      .getAttribute("aria-current")
  ).toBe("page");
  expect(within(drawer).getByText(i18n.t("common.mobile_navigation_more_on_desktop"))).toBeTruthy();
  expect(within(drawer).queryByRole("link", { name: i18n.t("common.workflows") })).toBeNull();
  expect(within(drawer).queryByRole("link", { name: i18n.t("common.settings") })).toBeNull();
});

test("choosing a page in the drawer goes there and closes it", async () => {
  stubScreen(true);
  const router = await renderTopBar();
  const drawer = await openDrawer();
  fireEvent.click(within(drawer).getByRole("link", { name: i18n.t("common.contacts") }));
  await waitFor(() => expect(router.state.location.pathname).toBe(`/workspaces/${WS}/contacts`));
  await waitFor(() =>
    expect(screen.queryByRole("dialog", { name: i18n.t("common.mobile_navigation_title") })).toBeNull()
  );
  expect(
    screen
      .getByRole("button", { name: i18n.t("common.mobile_open_navigation") })
      .getAttribute("aria-expanded")
  ).toBe("false");
});

test("choosing the page already open closes the drawer too", async () => {
  stubScreen(true);
  await renderTopBar();
  const drawer = await openDrawer();
  fireEvent.click(within(drawer).getByRole("link", { name: i18n.t("common.surveys") }));
  await waitFor(() =>
    expect(screen.queryByRole("dialog", { name: i18n.t("common.mobile_navigation_title") })).toBeNull()
  );
});

test("the billing role sees the drawer's entries disabled, as in the sidebar", async () => {
  stubScreen(true);
  await renderTopBar("billing");
  const drawer = await openDrawer();
  expect(within(drawer).queryAllByRole("link")).toEqual([]);
  expect(
    within(drawer)
      .getAllByRole("button", { name: i18n.t("common.surveys") })[0]
      ?.getAttribute("aria-disabled")
  ).toBe("true");
});

test("from the sm breakpoint up the drawer does not open: the sidebar is there", async () => {
  stubScreen(false);
  await renderTopBar();
  fireEvent.click(await screen.findByRole("button", { name: i18n.t("common.mobile_open_navigation") }));
  expect(screen.queryByRole("dialog", { name: i18n.t("common.mobile_navigation_title") })).toBeNull();
});

/** Opens a switcher in the top bar by its trigger's name and returns the names of its menu items. */
const openSwitcher = async (name: RegExp) => {
  fireEvent.keyDown(await screen.findByRole("button", { name }), { key: "Enter" });
  const menu = await screen.findByRole("menu");
  return within(menu)
    .getAllByRole("menuitemcheckbox")
    .map((item) => item.textContent);
};

test("on a phone the workspace switcher only switches: an owner is not offered a new workspace", async () => {
  stubScreen(true);
  await renderTopBar();
  expect(await openSwitcher(/Website/)).toEqual(["Website"]);
  expect(screen.queryByText(i18n.t("common.add_new_workspace"))).toBeNull();
  expect(screen.queryByText(i18n.t("common.create_workspace"))).toBeNull();
});

test("from the sm breakpoint up the workspace switcher offers an owner a new workspace", async () => {
  stubScreen(false);
  await renderTopBar();
  expect(await openSwitcher(/Website/)).toEqual(["Website", i18n.t("common.add_new_workspace")]);
});

test("on a phone the organization switcher is there, as its icon, and only switches to another organization's surveys", async () => {
  stubScreen(true);
  const router = await renderTopBar();
  const trigger = await screen.findByRole("button", { name: /Acme/ });
  expect(within(trigger).getByText("Acme").className).toBe("sr-only");
  expect(await openSwitcher(/Acme/)).toEqual(["Acme", "Globex"]);
  expect(screen.queryByText(i18n.t("workspace.settings.general.create_new_organization"))).toBeNull();
  expect(screen.queryByText(i18n.t("common.settings"))).toBeNull();

  // The current organization would open its settings, which a phone does not open: it stays on the page.
  fireEvent.click(screen.getByRole("menuitemcheckbox", { name: "Acme" }));
  expect(router.state.location.pathname).toBe(`/workspaces/${WS}/surveys`);

  fireEvent.keyDown(screen.getByRole("button", { name: /Acme/ }), { key: "Enter" });
  fireEvent.click(await screen.findByRole("menuitemcheckbox", { name: "Globex" }));
  await waitFor(() => expect(router.state.location.pathname).toBe(`/organizations/${OTHER_ORG}`));
});

test("from the sm breakpoint up the organization switcher shows its name and offers creating one and its settings", async () => {
  stubScreen(false);
  await renderTopBar();
  const trigger = await screen.findByRole("button", { name: /Acme/ });
  expect(within(trigger).getByText("Acme").className).toBe("");
  expect(await openSwitcher(/Acme/)).toEqual([
    "Acme",
    "Globex",
    i18n.t("workspace.settings.general.create_new_organization"),
    i18n.t("common.settings"),
  ]);
});
