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

const json = (body: unknown) =>
  new Response(JSON.stringify(body), { status: 200, headers: { "content-type": "application/json" } });

/** canCreate is the server's answer to whether the caller may create an organization. */
const renderBreadcrumb = (canCreate: boolean) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      const key = `${init.method ?? "GET"} ${path}`;
      if (key === "GET /api/app/organizations") {
        return json({ organizations: [{ id: ORG, name: "Acme", role: "member" }] });
      }
      if (key === "GET /api/app/organization-creation") return json({ allowed: canCreate });
      throw new Error(`unexpected ${key}`);
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
  // forma2 offered "Create new organization" only at the foot of the organization's settings, under Delete; the switcher offers it beside the list, as the workspace switcher offers "Add new workspace", whatever the caller's role here, whenever the server says they may create one.
  test("the switcher offers to create an organization, even to a member, and opens the create dialog", async () => {
    renderBreadcrumb(true);
    fireEvent.keyDown(await screen.findByRole("button", { name: /Acme/ }), { key: "Enter" });
    expect(await screen.findByRole("menuitemcheckbox", { name: "Acme" })).toBeTruthy();
    const dialog = document.querySelector("dialog");
    expect(dialog?.hasAttribute("open")).toBe(false);

    fireEvent.click(
      await screen.findByRole("menuitemcheckbox", {
        name: t("workspace.settings.general.create_new_organization"),
      })
    );
    expect(dialog?.hasAttribute("open")).toBe(true);
    expect(await screen.findByLabelText(t("workspace.settings.general.organization_name"))).toBeTruthy();
  });

  // On an invite-only instance only the owner of an organization may create another one; anyone else is not offered it.
  test("the switcher does not offer to create an organization to someone who may not", async () => {
    renderBreadcrumb(false);
    fireEvent.keyDown(await screen.findByRole("button", { name: /Acme/ }), { key: "Enter" });
    expect(await screen.findByRole("menuitemcheckbox", { name: "Acme" })).toBeTruthy();
    await waitFor(() =>
      expect(vi.mocked(fetch).mock.calls.some(([path]) => path === "/api/app/organization-creation")).toBe(
        true
      )
    );
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(
      screen.queryByRole("menuitemcheckbox", {
        name: t("workspace.settings.general.create_new_organization"),
      })
    ).toBeNull();
  });
});
