import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import type { WorkspaceDetail } from "../../lib/queries/workspaces";
import { AppConnectionPage } from "./app-connection";

const ORG = "0190a000-0000-7000-8000-000000000001";
const WS = "0190a000-0000-7000-8000-000000000002";
const APP_URL = "https://forma.example.com";

let i18n: I18n;
const t = (key: string) => i18n.t(key);

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const workspace = { id: WS, organizationId: ORG } as WorkspaceDetail;

const renderPage = () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      if (path !== `/api/app/workspaces/${WS}/app-connection`) throw new Error(`unexpected ${path}`);
      return new Response(JSON.stringify({ workspaceId: WS, appUrl: APP_URL, appSetupCompleted: false }), {
        status: 200,
        headers: { "content-type": "application/json" },
      });
    })
  );
  const rootRoute = createRootRoute({ component: Outlet });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({
        getParentRoute: () => rootRoute,
        path: "/sdk",
        component: () => <AppConnectionPage workspace={workspace} />,
      }),
    ]),
    history: createMemoryHistory({ initialEntries: ["/sdk"] }),
  });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
};

const methodButton = async (key: string) =>
  screen.findByRole("button", { name: new RegExp(t(`workspace.app-connection.install_method_${key}`)) });

// Every framework option used to start with "npm install @forma/js", a package that does not exist.
test("the website methods load the SDK with the script tag, and none installs a package", async () => {
  renderPage();
  fireEvent.click(await methodButton("website"));
  for (const option of ["html", "react", "nextjs", "vue"]) {
    fireEvent.click(
      screen.getByRole("radio", { name: t(`workspace.app-connection.install_method_${option}`) })
    );
    const text = document.body.textContent ?? "";
    expect(text).not.toMatch(/npm install|@forma\//);
    expect(text).toContain(WS);
    expect(text).toContain('"/js/forma.umd.js"');
  }
  expect(document.body.textContent).not.toMatch(/useSearchParams|usePathname/);
});

// The mobile SDKs, and the WordPress plugin the page sent people to (somebody else's), are not published.
test("mobile and WordPress say coming soon and offer nothing to install", async () => {
  renderPage();
  await methodButton("website");
  for (const key of ["mobile", "wordpress"]) {
    const title = screen.getByText(t(`workspace.app-connection.install_method_${key}`));
    expect(title.closest("button")).toBeNull();
    const card = title.closest("div.rounded-lg");
    expect(card).not.toBeNull();
    expect(within(card as HTMLElement).getByText(t("workspace.app-connection.coming_soon"))).toBeDefined();
  }
  expect(document.body.textContent).not.toMatch(/official plugin|@forma\/react-native|com\.forma:android/);
});

test("the page links to the SDK reference", async () => {
  renderPage();
  await methodButton("website");
  const link = screen.getByRole("link", { name: new RegExp(t("workspace.app-connection.loader_docs_link")) });
  expect(link.getAttribute("href")).toBe("https://forma.yldm.ai/developers/sdk/");
  expect(link.getAttribute("rel")).toContain("noopener");
});
