import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import { OrganizationBreadcrumb } from "./organization-breadcrumb";

const ORG = "0190a000-0000-7000-8000-000000000001";

let i18n: I18n;
const t = (key: string) => i18n.t(key);

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
});

const renderBreadcrumb = () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      const key = `${init.method ?? "GET"} ${path}`;
      if (key !== "GET /api/app/organizations") throw new Error(`unexpected ${key}`);
      return new Response(JSON.stringify({ organizations: [{ id: ORG, name: "Acme", role: "member" }] }), {
        status: 200,
        headers: { "content-type": "application/json" },
      });
    })
  );
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const rootRoute = createRootRoute({ component: Outlet });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({
        getParentRoute: () => rootRoute,
        path: "/",
        component: () => (
          <ol>
            <OrganizationBreadcrumb organizationId={ORG} organizationName="Acme" isLastCrumb />
          </ol>
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

describe("OrganizationBreadcrumb", () => {
  // forma2 offered "Create new organization" only at the foot of the organization's settings, under Delete; the switcher offers it beside the list, as the workspace switcher offers "Add new workspace", and to any role, since anyone signed in may create an organization.
  test("the switcher offers to create an organization, even to a member, and opens the create dialog", async () => {
    renderBreadcrumb();
    fireEvent.keyDown(await screen.findByRole("button", { name: /Acme/ }), { key: "Enter" });
    expect(await screen.findByRole("menuitemcheckbox", { name: "Acme" })).toBeTruthy();
    const dialog = document.querySelector("dialog");
    expect(dialog?.hasAttribute("open")).toBe(false);

    fireEvent.click(
      screen.getByRole("menuitemcheckbox", { name: t("workspace.settings.general.create_new_organization") })
    );
    expect(dialog?.hasAttribute("open")).toBe(true);
    expect(await screen.findByLabelText(t("workspace.settings.general.organization_name"))).toBeTruthy();
  });
});
