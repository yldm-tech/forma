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
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import { billingKeys } from "../../lib/queries/billing";
import { DEVELOPMENT_VERSION, instanceKeys, isNewerVersion } from "../../lib/queries/instance";
import { MainNavigation } from "./main-navigation";

const ORG = "0190a000-0000-7000-8000-000000000001";
const WS = "0190a000-0000-7000-8000-000000000002";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const notice = (version: string) => i18n.t("common.new_version_available", { version });

/** The workspace sidebar for a role on an instance running version, self-hosted or on Forma Cloud, whose latest release on GitHub is latest. Returns the latest-release lookup's mock and the query client. */
const renderNavigation = async ({
  role,
  version = "0.2.10",
  isFormaCloud = false,
  latest = "v0.2.11",
}: {
  role: string;
  version?: string;
  isFormaCloud?: boolean;
  latest?: string;
}) => {
  const lookup = vi.fn(async (path: string) => {
    if (path !== "/api/app/instance/latest-release") throw new Error(`unexpected ${path}`);
    return new Response(JSON.stringify({ tagName: latest }), {
      status: 200,
      headers: { "content-type": "application/json" },
    });
  });
  vi.stubGlobal("fetch", lookup);
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  // What the workspace layout has loaded by the time the sidebar shows.
  queryClient.setQueryData(instanceKeys.info, { storageConfigured: true, version });
  queryClient.setQueryData(billingKeys.summary(ORG), { isFormaCloud });
  const rootRoute = createRootRoute({ component: Outlet });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({
        getParentRoute: () => rootRoute,
        path: "/",
        component: () => (
          <MainNavigation workspaceId={WS} organizationId={ORG} membershipRole={role} pathname="/" />
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
  await waitFor(() => expect(router.state.status).toBe("idle"));
  await screen.findByText(i18n.t("common.settings"));
  return { lookup, queryClient };
};

describe("the new-version notice", () => {
  test.each(["owner", "manager"])("a self-hosted %s is told about a newer release", async (role) => {
    await renderNavigation({ role });
    const link = (await screen.findByText(notice("v0.2.11"))).closest("a");
    expect(link?.getAttribute("href")).toBe("https://github.com/yldm-tech/forma/releases");
    expect(link?.getAttribute("target")).toBe("_blank");
  });

  test.each([
    ["a member", { role: "member" }],
    ["a billing member", { role: "billing" }],
    ["Forma Cloud", { role: "owner", isFormaCloud: true }],
    ["a development build", { role: "owner", version: DEVELOPMENT_VERSION }],
  ])("%s never looks the release up", async (_name, options) => {
    const { lookup } = await renderNavigation(options);
    expect(lookup).not.toHaveBeenCalled();
    expect(screen.queryByText(notice("v0.2.11"))).toBeNull();
  });

  test("an instance already on the latest release shows nothing", async () => {
    const { lookup, queryClient } = await renderNavigation({ role: "owner", latest: "v0.2.10" });
    await waitFor(() => expect(queryClient.getQueryData(instanceKeys.latestRelease)).toBe("v0.2.10"));
    expect(lookup).toHaveBeenCalledTimes(1);
    expect(screen.queryByText(notice("v0.2.10"))).toBeNull();
  });
});

describe("isNewerVersion", () => {
  test("compares releases as forma2 did", () => {
    expect(isNewerVersion("v0.2.10", "v0.2.11")).toBe(true);
    expect(isNewerVersion("0.2.10", "v0.3.0")).toBe(true);
    expect(isNewerVersion("v0.2.10", "v0.2.10")).toBe(false);
    expect(isNewerVersion("v0.2.11", "v0.2.10")).toBe(false);
    expect(isNewerVersion("3.16", "3.16.0")).toBe(false);
    expect(isNewerVersion("1.0.0-beta.1", "1.0.0")).toBe(true);
    expect(isNewerVersion("1.0.0", "1.0.0-beta.1")).toBe(false);
    expect(isNewerVersion("3.17.0-rc.5", "3.17.0-rc.10")).toBe(true);
    expect(isNewerVersion("1.0.0-beta", "1.0.0-beta.1")).toBe(true);
    expect(isNewerVersion("1.0.0+001", "1.0.0+002")).toBe(false);
  });
});
