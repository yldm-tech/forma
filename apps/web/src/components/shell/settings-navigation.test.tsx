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
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import type { OrganizationRole } from "../../lib/roles";
import { SettingsNavigation } from "./settings-navigation";

const ORG = "0190a000-0000-7000-8000-000000000001";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(cleanup);

const t = (key: string) => i18n.t(key);

/** Renders the sidebar at path, as the settings shell does for the page there. */
const renderAt = async (path: string, membershipRole: OrganizationRole = "owner") => {
  const rootRoute = createRootRoute({ component: Outlet });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({
        getParentRoute: () => rootRoute,
        path: "$",
        component: () => (
          <SettingsNavigation
            organizationId={ORG}
            organizationName="Acme"
            membershipRole={membershipRole}
            backUrl="/"
            pathname={path}
          />
        ),
      }),
    ]),
    history: createMemoryHistory({ initialEntries: [path] }),
  });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
  await waitFor(() => expect(router.state.status).toBe("idle"));
};

const accountNav = () => screen.getByRole("navigation", { name: t("common.account") });

describe("SettingsNavigation", () => {
  test("has an account section whose entry for the open page is the current one", async () => {
    await renderAt("/account/settings/notifications");
    const nav = accountNav();
    const profile = within(nav).getByRole("link", { name: t("common.profile") });
    const notifications = within(nav).getByRole("link", { name: t("common.notifications") });
    expect(profile.getAttribute("href")).toBe("/account/settings/profile");
    expect(profile.getAttribute("aria-current")).toBeNull();
    expect(notifications.getAttribute("aria-current")).toBe("page");
    // No organization entry is current on an account page.
    const organizationNav = screen.getByRole("navigation", { name: t("common.settings") });
    expect(within(organizationNav).queryByRole("link", { current: "page" })).toBeNull();
  });

  test("marks the organization page current and names member management after members and teams", async () => {
    await renderAt(`/organizations/${ORG}/settings/teams`);
    const teams = screen.getByRole("link", { name: t("common.members_and_teams") });
    expect(teams.getAttribute("href")).toBe(`/organizations/${ORG}/settings/teams`);
    expect(teams.getAttribute("aria-current")).toBe("page");
    expect(within(accountNav()).queryByRole("link", { current: "page" })).toBeNull();
  });

  test("leaves notifications out for the billing role, as the user menu does", async () => {
    await renderAt("/account/settings/profile", "billing");
    const nav = accountNav();
    expect(
      within(nav)
        .getByRole("link", { name: t("common.profile") })
        .getAttribute("aria-current")
    ).toBe("page");
    expect(within(nav).queryByRole("link", { name: t("common.notifications") })).toBeNull();
  });
});
