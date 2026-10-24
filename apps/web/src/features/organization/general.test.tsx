import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { cleanup, render, screen } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import { billingKeys } from "../../lib/queries/billing";
import { instanceKeys } from "../../lib/queries/instance";
import type { OrganizationDetail } from "../../lib/queries/organizations";
import type { OrganizationRole } from "../../lib/roles";
import { OrganizationGeneralPage } from "./general";

const ORG = "0190a000-0000-7000-8000-000000000001";
const API = `/api/app/organizations/${ORG}`;

let i18n: I18n;
const t = (key: string) => i18n.t(key);

beforeAll(async () => {
  i18n = await initI18n("en-US");
  window.scrollTo = () => {};
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const organization: OrganizationDetail = {
  id: ORG,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
  name: "Acme",
  isAISmartToolsEnabled: false,
  displayTimeZone: null,
  surveySchedulingTimeZone: null,
  surveyScheduling: {
    timeZone: "Europe/Berlin",
    localHour: 0,
    localMinute: 0,
    instanceTimeZone: "Europe/Berlin",
  },
  whitelabel: {},
  membership: { organizationId: ORG, userId: "u1", accepted: true, role: "owner" },
  userManagementAllowed: true,
};

/** The server behind the page's other cards. */
const stubServer = () =>
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      const key = `${init.method ?? "GET"} ${path}`;
      const answers: Record<string, unknown> = {
        [`GET ${API}/ai`]: { isInstanceConfigured: false },
        [`GET ${API}/workspaces`]: { workspaces: [] },
        "GET /api/app/auth/me": { id: "u1", name: "Grace" },
        "GET /api/app/auth/options": { smtpConfigured: true },
      };
      if (!(key in answers)) throw new Error(`unexpected ${key}`);
      return new Response(JSON.stringify(answers[key]), {
        status: 200,
        headers: { "content-type": "application/json" },
      });
    })
  );

/** The page on an instance with or without file storage, self-hosted or on Forma Cloud, both already known as the settings shell would have loaded them. */
const renderPage = ({
  storageConfigured,
  isFormaCloud,
  role = "owner",
  overrides = {},
}: {
  storageConfigured: boolean;
  isFormaCloud: boolean;
  role?: OrganizationRole;
  overrides?: Partial<OrganizationDetail>;
}) => {
  stubServer();
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  queryClient.setQueryData(instanceKeys.info, { storageConfigured, version: "0.2.42" });
  queryClient.setQueryData(billingKeys.summary(ORG), { isFormaCloud });
  const rootRoute = createRootRoute({ component: Outlet });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({
        getParentRoute: () => rootRoute,
        path: "/",
        component: () => (
          <OrganizationGeneralPage
            organization={{ ...organization, ...overrides, membership: { ...organization.membership, role } }}
          />
        ),
      }),
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

describe("OrganizationGeneralPage", () => {
  test("self-hosted without file storage: the storage warning, the security list tip and the version", async () => {
    renderPage({ storageConfigured: false, isFormaCloud: false });
    expect(await screen.findByText(t("common.storage_not_configured"))).toBeTruthy();
    expect(await screen.findByText(t("workspace.settings.general.security_list_tip_link"))).toBeTruthy();
    expect(await screen.findByText(t("common.forma_version"))).toBeTruthy();
    expect(screen.getByText("0.2.42")).toBeTruthy();
    expect(screen.getByText(t("workspace.look.email_customization"))).toBeTruthy();
  });

  test("on Forma Cloud with file storage: neither the warning nor the tip", async () => {
    renderPage({ storageConfigured: true, isFormaCloud: true });
    expect(await screen.findByText("0.2.42")).toBeTruthy();
    expect(screen.queryByText(t("common.storage_not_configured"))).toBeNull();
    expect(screen.queryByText(t("workspace.settings.general.security_list_tip_link"))).toBeNull();
  });

  test.each(["member", "billing"] as const)(
    "a %s, who can change nothing, is told so once at the top instead of in every card",
    async (role) => {
      renderPage({ storageConfigured: true, isFormaCloud: true, role });
      expect(await screen.findByText(t("workspace.settings.general.view_only_access"))).toBeTruthy();
      expect(
        screen.queryByText(t("common.only_owners_managers_and_manage_access_members_can_perform_this_action"))
      ).toBeNull();
      expect(
        screen.queryByText(t("workspace.settings.general.only_org_owner_can_perform_action"))
      ).toBeNull();
      expect(
        (screen.getByLabelText(t("workspace.settings.general.organization_name")) as HTMLInputElement)
          .disabled
      ).toBe(true);
    }
  );

  test("the scheduling zone menu shows a stored zone the browser does not list, and is described by its explanation", async () => {
    const listed = Intl.supportedValuesOf("timeZone");
    const unlisted = ["Asia/Kolkata", "Europe/Kyiv", "Asia/Calcutta"].find((zone) => !listed.includes(zone));
    if (!unlisted) throw new Error("the browser lists every zone this test tries");
    renderPage({
      storageConfigured: true,
      isFormaCloud: true,
      overrides: {
        surveySchedulingTimeZone: unlisted,
        surveyScheduling: { ...organization.surveyScheduling, timeZone: unlisted },
      },
    });
    const menu = await screen.findByRole("combobox", {
      name: t("workspace.settings.general.survey_scheduling_time_zone"),
    });
    expect(menu.textContent).toContain(unlisted);
    const description = document.getElementById(menu.getAttribute("aria-describedby") ?? "");
    expect(description?.textContent).toContain("00:00");
  });

  test("a manager edits the page and is told only owners delete the organization", async () => {
    renderPage({ storageConfigured: true, isFormaCloud: true, role: "manager" });
    expect(
      await screen.findByText(t("workspace.settings.general.only_org_owner_can_perform_action"))
    ).toBeTruthy();
    expect(screen.queryByText(t("workspace.settings.general.view_only_access"))).toBeNull();
  });
});
