import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider, createMemoryHistory, createRouter } from "@tanstack/react-router";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import type { NotificationOrganization } from "../../lib/queries/auth";
import { routeTree } from "../../routeTree.gen";
import { Route as NotificationsRoute } from "../../routes/_authenticated/account/settings/notifications";

// An email's unsubscribe link opened on a phone: the notification settings stay under the phone overlay, so the link lands on a page a phone may open, which unsubscribes, says what, and offers an undo, through the same signed-in, CSRF-checked request as the settings page.

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
  window.scrollTo = () => {};
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const USER = { id: "u1", name: "Ada", email: "ada@example.com", locale: "en-US" };

const organizations: NotificationOrganization[] = [
  {
    id: "o1",
    name: "Acme",
    autoSubscribe: true,
    weeklySummary: true,
    surveys: [
      { id: "s1", name: "NPS Survey", workspaceId: "w1", workspaceName: "Website", alert: true },
      { id: "s2", name: "Onboarding", workspaceId: "w1", workspaceName: "Website", alert: false },
    ],
  },
];

const key = (name: string) => `workspace.settings.notifications.unsubscribe_page.${name}`;

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

const stubScreen = (isPhone: boolean) =>
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: isPhone && query === "(max-width: 639px)",
    addEventListener: () => {},
    removeEventListener: () => {},
  }));

/** The Go server: the session (or none), the notification settings, and a PATCH that answers with the given statuses in turn, then 204. Records each PATCH with its headers. */
const stubServer = ({ signedIn = true, patchStatuses = [] as number[] } = {}) => {
  const patches: { body: unknown; headers: Headers }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      if (path === "/api/app/auth/me") {
        return signedIn ? json(200, USER) : json(401, { code: "not_authenticated", message: "x" });
      }
      if (path === "/api/app/auth/me/notifications" && init.method === "PATCH") {
        patches.push({ body: JSON.parse(init.body as string), headers: new Headers(init.headers) });
        const status = patchStatuses.shift() ?? 204;
        return status === 204
          ? new Response(null, { status })
          : json(status, { code: "internal", message: "x" });
      }
      if (path === "/api/app/auth/me/notifications") return json(200, { organizations });
      if (path === "/api/app/auth/options") {
        return json(200, {
          emailAuthEnabled: true,
          signupEnabled: true,
          passwordResetEnabled: true,
          emailVerificationRequired: false,
          smtpConfigured: true,
          ssoProviders: [],
          isFormaCloud: false,
        });
      }
      return json(404, { code: "not_found", message: path });
    })
  );
  return patches;
};

const renderAt = async (href: string) => {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createRouter({
    routeTree,
    context: { queryClient },
    history: createMemoryHistory({ initialEntries: [href] }),
  });
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

const heading = (name: string) => screen.findByRole("heading", { level: 1, name });

describe("an unsubscribe link on a phone", () => {
  test("lands on the unsubscribe page, outside the overlay, which turns the survey's alert off and can undo it", async () => {
    stubScreen(true);
    const patches = stubServer();
    const router = await renderAt("/account/settings/notifications?type=alert&elementId=s1");

    expect(await heading(i18n.t(key("unsubscribed")))).toBeTruthy();
    expect(router.state.location.pathname).toBe("/account/unsubscribe");
    expect(router.state.location.search).toEqual({ type: "alert", elementId: "s1" });
    expect(screen.queryByTestId("no-mobile-overlay")).toBeNull();
    expect(screen.getByText(i18n.t(key("alert_off"), { survey: "NPS Survey" }))).toBeTruthy();
    expect(patches.map((patch) => patch.body)).toEqual([{ type: "alert", id: "s1", enabled: false }]);
    expect(patches[0]?.headers.get("X-Requested-With")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: i18n.t(key("undo")) }));
    expect(await heading(i18n.t(key("resubscribed")))).toBeTruthy();
    expect(screen.getByText(i18n.t(key("alert_on"), { survey: "NPS Survey" }))).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: i18n.t(key("unsubscribe")) }));
    expect(await heading(i18n.t(key("unsubscribed")))).toBeTruthy();
    expect(patches.map((patch) => patch.body)).toEqual([
      { type: "alert", id: "s1", enabled: false },
      { type: "alert", id: "s1", enabled: true },
      { type: "alert", id: "s1", enabled: false },
    ]);
  });

  test("stops auto-subscribing to an organization's new surveys", async () => {
    stubScreen(true);
    const patches = stubServer();
    await renderAt("/account/settings/notifications?type=unsubscribedOrganizationIds&elementId=o1");

    expect(await heading(i18n.t(key("unsubscribed")))).toBeTruthy();
    expect(screen.getByText(i18n.t(key("auto_subscribe_off"), { organization: "Acme" }))).toBeTruthy();
    expect(patches.map((patch) => patch.body)).toEqual([
      { type: "unsubscribedOrganizationIds", id: "o1", enabled: false },
    ]);
  });

  test("turns an organization's weekly summary off", async () => {
    stubScreen(true);
    const patches = stubServer();
    await renderAt(
      "/account/settings/notifications?type=unsubscribedWeeklySummaryOrganizationIds&elementId=o1"
    );

    expect(await heading(i18n.t(key("unsubscribed")))).toBeTruthy();
    expect(screen.getByText(i18n.t(key("weekly_summary_off"), { organization: "Acme" }))).toBeTruthy();
    expect(patches.map((patch) => patch.body)).toEqual([
      { type: "unsubscribedWeeklySummaryOrganizationIds", id: "o1", enabled: false },
    ]);
  });

  test("says a switch already off is off, changes nothing, and offers to turn it back on", async () => {
    stubScreen(true);
    const patches = stubServer();
    await renderAt("/account/unsubscribe?type=alert&elementId=s2");

    expect(await heading(i18n.t(key("already_unsubscribed")))).toBeTruthy();
    expect(screen.getByText(i18n.t(key("alert_off"), { survey: "Onboarding" }))).toBeTruthy();
    expect(patches).toEqual([]);

    fireEvent.click(screen.getByRole("button", { name: i18n.t(key("subscribe_again")) }));
    expect(await heading(i18n.t(key("resubscribed")))).toBeTruthy();
    expect(patches.map((patch) => patch.body)).toEqual([{ type: "alert", id: "s2", enabled: true }]);
  });

  test("says a link to a survey this account does not have no longer applies, names the account, and changes nothing", async () => {
    stubScreen(true);
    const patches = stubServer();
    await renderAt("/account/unsubscribe?type=alert&elementId=gone");

    expect(await heading(i18n.t(key("link_no_longer_applies")))).toBeTruthy();
    // The cause may be the account rather than the survey: the email may have gone to another account than the one signed in on this phone.
    const description = screen.getByText(
      i18n.t(key("link_no_longer_applies_description"), { email: USER.email })
    );
    expect(description.textContent).toContain(USER.email);
    expect(screen.queryByRole("button")).toBeNull();
    expect(patches).toEqual([]);
  });

  test("says nothing changed when the save fails, and tries again", async () => {
    stubScreen(true);
    const patches = stubServer({ patchStatuses: [500] });
    await renderAt("/account/unsubscribe?type=alert&elementId=s1");

    expect(await heading(i18n.t(key("not_changed")))).toBeTruthy();
    expect(screen.getByRole("alert").textContent).toBe(
      i18n.t("common.something_went_wrong_please_try_again")
    );

    fireEvent.click(screen.getByRole("button", { name: i18n.t("common.try_again") }));
    expect(await heading(i18n.t(key("unsubscribed")))).toBeTruthy();
    expect(patches.map((patch) => patch.body)).toEqual([
      { type: "alert", id: "s1", enabled: false },
      { type: "alert", id: "s1", enabled: false },
    ]);
  });

  test("links to the full settings, which open on a computer, and names the account", async () => {
    stubScreen(true);
    stubServer();
    await renderAt("/account/unsubscribe?type=alert&elementId=s1");

    const settings = await screen.findByRole("link", { name: i18n.t(key("all_settings")) });
    expect(settings.getAttribute("href")).toBe("/account/settings/notifications");
    expect(screen.getByText(i18n.t(key("all_settings_on_computer")))).toBeTruthy();
    expect(
      screen.getByText(i18n.t("common.mobile_overlay_signed_in_as", { email: USER.email }))
    ).toBeTruthy();
  });

  test("still needs a session: a signed-out visitor goes to sign in and comes back to the link", async () => {
    stubScreen(true);
    const patches = stubServer({ signedIn: false });
    const router = await renderAt("/account/unsubscribe?type=alert&elementId=s1");

    await waitFor(() => expect(router.state.location.pathname).toBe("/auth/login"));
    expect(String(router.state.location.search.callbackUrl)).toContain(
      "/app/account/unsubscribe?type=alert&elementId=s1"
    );
    expect(patches).toEqual([]);
  });
});

describe("the notification settings", () => {
  const beforeLoad = NotificationsRoute.options.beforeLoad as unknown as (ctx: {
    search: { type?: string; elementId?: string };
  }) => void;

  /** Runs the route's hook and returns where it redirected to, or null when it let the page load. */
  const redirectOf = (search: { type?: string; elementId?: string }): string | null => {
    try {
      beforeLoad({ search });
      return null;
    } catch (error) {
      const options = (error as { options?: { to?: string; search?: unknown } }).options;
      if (options?.to === undefined) throw error;
      return `${options.to} ${JSON.stringify(options.search)}`;
    }
  };

  test("keep an unsubscribe link on a bigger screen, where they unsubscribe in place", () => {
    stubScreen(false);
    expect(redirectOf({ type: "alert", elementId: "s1" })).toBeNull();
  });

  test("send an unsubscribe link on a phone to the unsubscribe page, and stay put without one", () => {
    stubScreen(true);
    expect(redirectOf({ type: "unsubscribedWeeklySummaryOrganizationIds", elementId: "o1" })).toBe(
      `/account/unsubscribe ${JSON.stringify({ type: "unsubscribedWeeklySummaryOrganizationIds", elementId: "o1" })}`
    );
    expect(redirectOf({})).toBeNull();
    expect(redirectOf({ type: "alert" })).toBeNull();
  });
});
