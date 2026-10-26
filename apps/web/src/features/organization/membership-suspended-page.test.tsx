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
import { switcherOrganizations } from "../../lib/navigation";
import { authKeys } from "../../lib/queries/auth";
import { type OrganizationDetail, organizationKeys } from "../../lib/queries/organizations";
import type { OrganizationRole } from "../../lib/roles";
import { MEMBERSHIP_SUSPENDED_CODE, guardPageError, membershipSuspendedPath } from "../../lib/route-guards";
import { toast } from "../../lib/toast";
import { MembershipSuspendedPage } from "./membership-suspended-page";

// The notice a member whose access to an organization is suspended lands on, the route guard that sends them there once the server refuses them, and how the organization switchers mark such an organization.

const ORG = "0190a000-0000-7000-8000-000000000001";
const OTHER = "0190a000-0000-7000-8000-000000000002";
const PAUSED = "0190a000-0000-7000-8000-000000000003";
const API = `/api/app/organizations/${ORG}`;
const SUSPENDED_AT = "2026-10-01T00:00:00Z";

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
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
});

const organization = (
  role: OrganizationRole,
  suspendedAt: string | null = SUSPENDED_AT
): OrganizationDetail => ({
  id: ORG,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
  name: "Acme",
  isAISmartToolsEnabled: false,
  displayTimeZone: null,
  surveySchedulingTimeZone: null,
  surveyScheduling: { timeZone: "UTC", localHour: 0, localMinute: 0, instanceTimeZone: "UTC" },
  whitelabel: {},
  membership: { organizationId: ORG, userId: "u1", accepted: true, role, suspendedAt },
  userManagementAllowed: role === "owner" || role === "manager",
  twoFactorEnforcedAt: null,
  twoFactorStatus: "enabled",
  twoFactorBlocked: false,
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

const renderInRouter = (node: ReactNode, queryClient: QueryClient) => {
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
};

const summaries = [
  { ...organization("member"), role: "member", suspendedAt: SUSPENDED_AT },
  { ...organization("owner"), id: OTHER, name: "Elsewhere", role: "owner", suspendedAt: null },
  { ...organization("member"), id: PAUSED, name: "Paused Inc", role: "member", suspendedAt: SUSPENDED_AT },
];

const renderPage = (detail: OrganizationDetail, answers: Record<string, unknown> = {}) => {
  const fetch = stubServer({ ...answers, "GET /api/app/organizations": { organizations: summaries } });
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
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
  queryClient.setQueryData(organizationKeys.detail(ORG), detail);
  renderInRouter(<MembershipSuspendedPage organizationId={ORG} />, queryClient);
  return fetch;
};

describe("MembershipSuspendedPage", () => {
  test("names the organization, says who restores the access, and offers the member's other organizations", async () => {
    renderPage(organization("member"));
    expect(
      await screen.findByRole("heading", {
        name: t("organizations.suspended.page_title", { organization: "Acme" }),
      })
    ).toBeTruthy();
    expect(document.querySelector("svg.lucide-user-x")).not.toBeNull();
    expect(
      screen.getByText(t("organizations.suspended.page_description", { organization: "Acme" }))
    ).toBeTruthy();
    expect((await screen.findByRole("link", { name: "Elsewhere" })).getAttribute("href")).toBe(
      `/organizations/${OTHER}`
    );
    // Another organization that suspended the member leads nowhere useful, so it is not offered.
    expect(screen.queryByRole("link", { name: "Paused Inc" })).toBeNull();
  });

  test("a suspended member may leave the organization", async () => {
    const fetch = renderPage(organization("member"), { [`POST ${API}/leave`]: {} });
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
    renderPage(organization("owner"));
    await screen.findByRole("link", { name: "Elsewhere" });
    expect(
      screen.queryByRole("button", { name: t("workspace.settings.general.leave_organization") })
    ).toBeNull();
  });

  test("once the access is restored, the member continues to the organization", async () => {
    renderPage(organization("member", null));
    expect(
      await screen.findByRole("link", {
        name: t("organizations.suspended.page_continue", { organization: "Acme" }),
      })
    ).toBeTruthy();
    // The restored notice does not keep the suspended glyph.
    expect(document.querySelector("svg.lucide-user-check")).not.toBeNull();
    expect(document.querySelector("svg.lucide-user-x")).toBeNull();
    expect(
      screen.queryByRole("button", { name: t("workspace.settings.general.leave_organization") })
    ).toBeNull();
  });
});

test("guardPageError sends a suspended member to the notice", () => {
  const refusal = new ApiError(403, MEMBERSHIP_SUSPENDED_CODE, "suspended", { organizationId: ORG });
  let thrown: unknown;
  try {
    guardPageError(refusal);
  } catch (error) {
    thrown = error;
  }
  expect(thrown).toMatchObject({ options: { href: membershipSuspendedPath(ORG) } });
});

test("switchers mark an organization that suspended the member", () => {
  const items = switcherOrganizations(i18n.t, [
    { id: ORG, name: "Acme", suspendedAt: SUSPENDED_AT },
    { id: OTHER, name: "Elsewhere", suspendedAt: null },
  ]);
  expect(items.map((o) => o.name)).toEqual([
    t("organizations.suspended.switcher_label", { organization: "Acme" }),
    "Elsewhere",
  ]);
});
