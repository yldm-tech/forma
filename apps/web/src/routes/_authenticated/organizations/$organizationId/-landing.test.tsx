import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider, createMemoryHistory, createRouter } from "@tanstack/react-router";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../../lib/i18n";
import { routeTree } from "../../../../routeTree.gen";

// The landing page of a member who can open no workspace: it names both ways forward and offers both; a phone opens it but, being read-only, offers only an organization of their own. On an invite-only instance, where only the owner of an organization may create another one, anyone else is told to ask for access instead.

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
  window.scrollTo = () => {};
  // jsdom has no modal dialogs.
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

const ORG = "0190a000-0000-7000-8000-000000000001";
const LANDING = `/organizations/${ORG}/landing`;

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

const renderLanding = async ({
  canCreateWorkspace,
  phone,
  canCreateOrganization = true,
}: {
  canCreateWorkspace: boolean;
  phone: boolean;
  /** "unknown endpoint" is an older server, as during a rolling deploy, that answers the question with 404. */
  canCreateOrganization?: boolean | "unknown endpoint";
}) => {
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: phone && query === "(max-width: 639px)",
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  }));
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      if (path === "/api/app/auth/me") {
        return json(200, { id: "u1", name: "Ada", email: "ada@example.com", locale: "en-US" });
      }
      if (path === "/api/app/organization-creation") {
        return canCreateOrganization === "unknown endpoint"
          ? json(404, { code: "not_found", message: "Not found" })
          : json(200, { allowed: canCreateOrganization });
      }
      if (path === `/api/app/organizations/${ORG}/navigation`)
        return json(200, { path: LANDING, canCreateWorkspace });
      if (path === `/api/app/organizations/${ORG}`) {
        return json(200, {
          id: ORG,
          name: "Acme",
          membership: {
            organizationId: ORG,
            userId: "u1",
            accepted: true,
            role: "member",
            suspendedAt: null,
          },
        });
      }
      throw new Error(`unexpected ${path}`);
    })
  );
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createRouter({
    routeTree,
    context: { queryClient },
    history: createMemoryHistory({ initialEntries: [LANDING] }),
  });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
  await router.load();
};

const createOrganization = () =>
  screen.findByRole("button", { name: i18n.t("workspace.settings.general.create_new_organization") });

describe("the organization landing page", () => {
  test("a member who cannot create a workspace is offered an organization of their own", async () => {
    await renderLanding({ canCreateWorkspace: false, phone: false });
    expect(
      await screen.findByText(i18n.t("organizations.landing.no_workspaces_warning_subtitle"))
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: i18n.t("common.create_workspace") })).toBeNull();
    fireEvent.click(await createOrganization());
    expect(await screen.findByLabelText(i18n.t("workspace.settings.general.organization_name"))).toBeTruthy();
  });

  test("a member who may create a workspace is offered both", async () => {
    await renderLanding({ canCreateWorkspace: true, phone: false });
    expect(await screen.findByRole("button", { name: i18n.t("common.create_workspace") })).toBeTruthy();
    expect(await createOrganization()).toBeTruthy();
  });

  test("opens on a phone instead of under the overlay", async () => {
    await renderLanding({ canCreateWorkspace: false, phone: true });
    const button = await createOrganization();
    expect(button.closest("[inert]")).toBeNull();
  });

  test("on a phone, a member who may create a workspace is offered only an organization", async () => {
    await renderLanding({ canCreateWorkspace: true, phone: true });
    const button = await createOrganization();
    expect(button.closest("[inert]")).toBeNull();
    expect(screen.queryByRole("button", { name: i18n.t("common.create_workspace") })).toBeNull();
  });

  test("on an invite-only instance, a member who may not create an organization is told to ask for access", async () => {
    await renderLanding({ canCreateWorkspace: false, phone: false, canCreateOrganization: false });
    expect(await screen.findByText(i18n.t("organization_creation.landing_ask_for_access"))).toBeTruthy();
    expect(screen.queryByText(i18n.t("organizations.landing.no_workspaces_warning_subtitle"))).toBeNull();
    expect(
      screen.queryByRole("button", { name: i18n.t("workspace.settings.general.create_new_organization") })
    ).toBeNull();
    expect(screen.queryByRole("button", { name: i18n.t("common.create_workspace") })).toBeNull();
  });

  test("a server that does not answer whether an organization may be created still shows the page, without offering one", async () => {
    await renderLanding({
      canCreateWorkspace: true,
      phone: false,
      canCreateOrganization: "unknown endpoint",
    });
    expect(await screen.findByRole("button", { name: i18n.t("common.create_workspace") })).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: i18n.t("workspace.settings.general.create_new_organization") })
    ).toBeNull();
  });

  test("on an invite-only instance, a manager is still offered a workspace but not an organization", async () => {
    await renderLanding({ canCreateWorkspace: true, phone: false, canCreateOrganization: false });
    expect(await screen.findByRole("button", { name: i18n.t("common.create_workspace") })).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: i18n.t("workspace.settings.general.create_new_organization") })
    ).toBeNull();
  });
});
