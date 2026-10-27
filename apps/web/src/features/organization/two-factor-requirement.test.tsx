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
import type { i18n as I18n } from "i18next";
import type { ReactNode } from "react";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { ApiError } from "../../lib/api";
import { initI18n } from "../../lib/i18n";
import { authKeys } from "../../lib/queries/auth";
import { type OrganizationDetail, organizationKeys } from "../../lib/queries/organizations";
import type { OrganizationRole } from "../../lib/roles";
import { TWO_FACTOR_REQUIRED_CODE, guardPageError, twoFactorRequiredPath } from "../../lib/route-guards";
import { toast } from "../../lib/toast";
import { TwoFactorRequiredPage } from "./two-factor-required-page";
import { TwoFactorReminder, TwoFactorRequirementCard } from "./two-factor-requirement";

// The organization's two-factor requirement: the owner's card on the general settings page, the reminder during the grace period, the page members are sent to, and the route guard that sends them there once the server refuses them.

const ORG = "0190a000-0000-7000-8000-000000000001";
const OTHER = "0190a000-0000-7000-8000-000000000002";
const API = `/api/app/organizations/${ORG}`;
const NEXT_WEEK = new Date(Date.now() + 7 * 24 * 3600_000).toISOString();
const LAST_WEEK = new Date(Date.now() - 7 * 24 * 3600_000).toISOString();

let i18n: I18n;
const t = (key: string, options?: Record<string, unknown>) => i18n.t(key, options);

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
  localStorage.clear();
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
});

const organization = (
  role: OrganizationRole,
  overrides: Partial<OrganizationDetail> = {}
): OrganizationDetail => ({
  id: ORG,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
  name: "Acme",
  isAISmartToolsEnabled: false,
  isAIResponseReadingEnabled: true,
  displayTimeZone: null,
  surveySchedulingTimeZone: null,
  surveyScheduling: { timeZone: "UTC", localHour: 0, localMinute: 0, instanceTimeZone: "UTC" },
  whitelabel: {},
  membership: { organizationId: ORG, userId: "u1", accepted: true, role, suspendedAt: null },
  userManagementAllowed: role === "owner" || role === "manager",
  twoFactorEnforcedAt: null,
  twoFactorStatus: "enabled",
  twoFactorBlocked: false,
  ...overrides,
});

/** The server: answers by method and path, recording every request. */
const stubServer = (answers: Record<string, unknown>) => {
  const fetch = vi.fn(async (path: string, init: RequestInit = {}) => {
    const key = `${init.method ?? "GET"} ${path}`;
    if (!(key in answers)) throw new Error(`unexpected ${key}`);
    return new Response(JSON.stringify(answers[key]), {
      status: 200,
      headers: { "content-type": "application/json" },
    });
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
};

const renderInRouter = (
  node: ReactNode,
  queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
) => {
  const rootRoute = createRootRoute({ component: Outlet });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({ getParentRoute: () => rootRoute, path: "/", component: () => node }),
    ]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
  return router;
};

const bodyOf = (fetch: ReturnType<typeof stubServer>, method: string, path: string): unknown => {
  const call = fetch.mock.calls.find(([p, init]) => p === path && init?.method === method);
  return call ? JSON.parse(call[1]?.body as string) : undefined;
};

describe("TwoFactorRequirementCard", () => {
  test("an owner without two-factor authentication is told to turn it on first", async () => {
    stubServer({});
    renderInRouter(
      <TwoFactorRequirementCard organization={organization("owner", { twoFactorStatus: "disabled" })} />
    );
    expect(await screen.findByText(t("organizations.two_factor.enable_own_first"))).toBeTruthy();
    const require = screen.getByRole("button", { name: t("organizations.two_factor.require_button") });
    expect((require as HTMLButtonElement).disabled).toBe(true);
  });

  test("an owner requires it with the chosen grace period", async () => {
    const fetch = stubServer({ [`PATCH ${API}`]: organization("owner") });
    renderInRouter(<TwoFactorRequirementCard organization={organization("owner")} />);
    expect(await screen.findByText(t("organizations.two_factor.optional_status"))).toBeTruthy();
    fireEvent.change(screen.getByLabelText(t("organizations.two_factor.grace_period_label")), {
      target: { value: "14" },
    });
    fireEvent.click(screen.getByRole("button", { name: t("organizations.two_factor.require_button") }));
    await waitFor(() =>
      expect(bodyOf(fetch, "PATCH", API)).toEqual({ requireTwoFactor: true, twoFactorGracePeriodDays: 14 })
    );
  });

  test("once it is required, owners and managers see who is missing and owners can stop requiring it", async () => {
    const fetch = stubServer({
      [`GET ${API}/members`]: {
        members: [
          {
            userId: "u1",
            name: "A",
            email: "a@example.com",
            accepted: true,
            role: "owner",
            isActive: true,
            suspendedAt: null,
            twoFactor: "enabled",
          },
          {
            userId: "u2",
            name: "B",
            email: "b@example.com",
            accepted: true,
            role: "member",
            isActive: true,
            suspendedAt: null,
            twoFactor: "disabled",
          },
          {
            userId: "u3",
            name: "C",
            email: "c@example.com",
            accepted: true,
            role: "member",
            isActive: true,
            suspendedAt: null,
            twoFactor: "sso",
          },
        ],
      },
      [`PATCH ${API}`]: organization("owner"),
    });
    renderInRouter(
      <TwoFactorRequirementCard organization={organization("owner", { twoFactorEnforcedAt: NEXT_WEEK })} />
    );
    expect(await screen.findByText(t("organizations.two_factor.missing_count", { count: 1 }))).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: t("organizations.two_factor.stop_button") }));
    await waitFor(() => expect(bodyOf(fetch, "PATCH", API)).toEqual({ requireTwoFactor: false }));
    cleanup();

    stubServer({ [`GET ${API}/members`]: { members: [] } });
    renderInRouter(
      <TwoFactorRequirementCard organization={organization("manager", { twoFactorEnforcedAt: LAST_WEEK })} />
    );
    expect(await screen.findByText(t("organizations.two_factor.owner_only"))).toBeTruthy();
    expect(screen.queryByRole("button", { name: t("organizations.two_factor.stop_button") })).toBeNull();
  });
});

describe("TwoFactorReminder", () => {
  test("reminds a member without it during the grace period until put away for the day", async () => {
    renderInRouter(
      <TwoFactorReminder
        organization={organization("member", { twoFactorEnforcedAt: NEXT_WEEK, twoFactorStatus: "disabled" })}
      />
    );
    expect(await screen.findByText(t("organizations.two_factor.reminder_title"))).toBeTruthy();
    const link = screen.getByRole("link", { name: t("organizations.two_factor.reminder_action") });
    expect(link.getAttribute("href")).toBe(twoFactorRequiredPath(ORG));
    fireEvent.click(screen.getByRole("button", { name: t("common.close") }));
    expect(screen.queryByText(t("organizations.two_factor.reminder_title"))).toBeNull();
    cleanup();

    // The next page the same day does not bring it back.
    renderInRouter(
      <>
        <p>page</p>
        <TwoFactorReminder
          organization={organization("member", {
            twoFactorEnforcedAt: NEXT_WEEK,
            twoFactorStatus: "disabled",
          })}
        />
      </>
    );
    await screen.findByText("page");
    expect(screen.queryByText(t("organizations.two_factor.reminder_title"))).toBeNull();
  });

  test("stays away for the rest of the user's own day, not the UTC one", async () => {
    const zone = process.env.TZ;
    process.env.TZ = "Asia/Tokyo";
    vi.useFakeTimers({ toFake: ["Date"] });
    try {
      const member = organization("member", {
        twoFactorEnforcedAt: "2026-11-30T00:00:00Z",
        twoFactorStatus: "disabled",
      });
      const renderReminder = async () => {
        renderInRouter(
          <>
            <p>page</p>
            <TwoFactorReminder organization={member} />
          </>
        );
        await screen.findByText("page");
      };

      // 08:30 in Tokyo, still the previous day in UTC.
      vi.setSystemTime(new Date("2026-10-25T23:30:00Z"));
      expect(new Date().getHours()).toBe(8);
      await renderReminder();
      fireEvent.click(screen.getByRole("button", { name: t("common.close") }));
      cleanup();

      // 09:00 in Tokyo the same day, after midnight UTC.
      vi.setSystemTime(new Date("2026-10-26T00:00:00Z"));
      await renderReminder();
      expect(screen.queryByText(t("organizations.two_factor.reminder_title"))).toBeNull();
      cleanup();

      // The next morning in Tokyo it is back.
      vi.setSystemTime(new Date("2026-10-26T23:30:00Z"));
      await renderReminder();
      expect(screen.getByText(t("organizations.two_factor.reminder_title"))).toBeTruthy();
    } finally {
      vi.useRealTimers();
      if (zone === undefined) delete process.env.TZ;
      else process.env.TZ = zone;
    }
  });

  test("says nothing to members who have it, are exempt, or whose organization does not require it", async () => {
    for (const overrides of [
      { twoFactorEnforcedAt: NEXT_WEEK, twoFactorStatus: "enabled" as const },
      { twoFactorEnforcedAt: NEXT_WEEK, twoFactorStatus: "sso" as const },
      { twoFactorEnforcedAt: null, twoFactorStatus: "disabled" as const },
    ]) {
      renderInRouter(
        <>
          <p>page</p>
          <TwoFactorReminder organization={organization("member", overrides)} />
        </>
      );
      await screen.findByText("page");
      expect(screen.queryByText(t("organizations.two_factor.reminder_title"))).toBeNull();
      cleanup();
    }
  });
});

describe("TwoFactorRequiredPage", () => {
  const renderPage = (overrides: Partial<OrganizationDetail>, answers: Record<string, unknown> = {}) => {
    const fetch = stubServer({
      ...answers,
      "GET /api/app/organizations": {
        organizations: [
          { ...organization("member"), role: "member" },
          { ...organization("owner"), id: OTHER, name: "Elsewhere", role: "owner" },
        ],
      },
    });
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    });
    queryClient.setQueryData(authKeys.me, {
      id: "u1",
      name: "Ada",
      email: "ada@example.com",
      emailVerified: true,
      locale: "en-US",
      identityProvider: "email",
      hasPassword: true,
      twoFactorEnabled: false,
      createdAt: "2026-01-01T00:00:00Z",
      lastLoginAt: null,
    });
    queryClient.setQueryData(organizationKeys.detail(ORG), organization("member", overrides));
    renderInRouter(<TwoFactorRequiredPage organizationId={ORG} />, queryClient);
    return fetch;
  };

  test("a refused member sets it up or opens another organization, but cannot go on", async () => {
    renderPage({ twoFactorEnforcedAt: LAST_WEEK, twoFactorStatus: "disabled", twoFactorBlocked: true });
    expect(await screen.findByText(t("organizations.two_factor.page_blocked"))).toBeTruthy();
    expect(screen.queryByRole("link", { name: t("organizations.two_factor.page_later") })).toBeNull();
    expect((await screen.findByRole("link", { name: "Elsewhere" })).getAttribute("href")).toBe(
      `/organizations/${OTHER}`
    );
    fireEvent.click(screen.getByRole("button", { name: t("organizations.two_factor.page_setup") }));
    expect(
      await screen.findByText(t("workspace.settings.profile.enable_two_factor_authentication"))
    ).toBeTruthy();
  });

  test("a refused member may leave the organization instead", async () => {
    const fetch = renderPage(
      { twoFactorEnforcedAt: LAST_WEEK, twoFactorStatus: "disabled", twoFactorBlocked: true },
      { [`POST ${API}/leave`]: {} }
    );
    fireEvent.click(
      await screen.findByRole("button", { name: t("workspace.settings.general.leave_organization") })
    );
    const confirm = await screen.findByRole("button", {
      name: t("workspace.settings.general.leave_organization_ok_btn_text"),
    });
    await waitFor(() => expect((confirm as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(confirm);
    await waitFor(() =>
      expect(
        fetch.mock.calls.some(([path, init]) => path === `${API}/leave` && init?.method === "POST")
      ).toBe(true)
    );
  });

  test("an owner, who cannot leave, is not offered it", async () => {
    renderPage({
      twoFactorEnforcedAt: LAST_WEEK,
      twoFactorStatus: "disabled",
      twoFactorBlocked: true,
      membership: { organizationId: ORG, userId: "u1", accepted: true, role: "owner", suspendedAt: null },
    });
    await screen.findByText(t("organizations.two_factor.page_blocked"));
    expect(
      screen.queryByRole("button", { name: t("workspace.settings.general.leave_organization") })
    ).toBeNull();
  });

  test("during the grace period a member may go on for now", async () => {
    renderPage({ twoFactorEnforcedAt: NEXT_WEEK, twoFactorStatus: "disabled" });
    const later = await screen.findByRole("link", { name: t("organizations.two_factor.page_later") });
    expect(later.getAttribute("href")).toBe(`/organizations/${ORG}`);
  });

  test("once it is on, the member continues to the organization", async () => {
    renderPage({ twoFactorEnforcedAt: LAST_WEEK, twoFactorStatus: "enabled" });
    expect(
      await screen.findByRole("link", {
        name: t("organizations.two_factor.page_continue", { organization: "Acme" }),
      })
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: t("organizations.two_factor.page_setup") })).toBeNull();
  });
});

describe("guardPageError", () => {
  test("sends a member the requirement refuses to set it up", () => {
    const refusal = new ApiError(403, TWO_FACTOR_REQUIRED_CODE, "requires 2FA", { organizationId: ORG });
    let thrown: unknown;
    try {
      guardPageError(refusal);
    } catch (error) {
      thrown = error;
    }
    expect(thrown).toMatchObject({ options: { href: twoFactorRequiredPath(ORG) } });
  });
});
