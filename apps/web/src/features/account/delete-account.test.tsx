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
import type { Me } from "../../lib/queries/auth";
import { DeleteAccountModal, type SoleOwnedOrganization } from "./delete-account";

const ORG = "0190a000-0000-7000-8000-000000000001";

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
});

const user = (identityProvider: Me["identityProvider"]): Me => ({
  id: "u1",
  name: "Ada",
  email: "ada@example.com",
  emailVerified: true,
  locale: "en-US",
  identityProvider,
  twoFactorEnabled: false,
  createdAt: "2026-01-01T00:00:00Z",
  lastLoginAt: null,
});

/** The server: the deletion preview and the instance's options. */
const stubServer = (organizations: SoleOwnedOrganization[], smtpConfigured = true) =>
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      const answers: Record<string, unknown> = {
        "/api/app/auth/deletion-preview": { organizations },
        "/api/app/auth/options": { smtpConfigured },
      };
      if (!(path in answers)) throw new Error(`unexpected ${path}`);
      return new Response(JSON.stringify(answers[path]), {
        status: 200,
        headers: { "content-type": "application/json" },
      });
    })
  );

const renderModal = (me: Me) => {
  const rootRoute = createRootRoute({ component: Outlet });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({
        getParentRoute: () => rootRoute,
        path: "/",
        component: () => <DeleteAccountModal user={me} onClose={() => {}} />,
      }),
    ]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
};

const deleteButton = () => screen.getByRole("button", { name: t("common.delete") }) as HTMLButtonElement;

describe("DeleteAccountModal", () => {
  test("names every organization that goes with the account, with the way to keep it", async () => {
    stubServer([
      { id: ORG, name: "Seed Organization", workspaceCount: 2 },
      { id: "o2", name: "Acme Research", workspaceCount: 1 },
    ]);
    renderModal(user("email"));

    const item = await screen.findByText(
      t("workspace.settings.profile.sole_owned_organization_item", { name: "Seed Organization", count: 2 }),
      { exact: false }
    );
    expect(item.textContent).toContain("Seed Organization (2 workspaces)");
    expect(
      screen.getByText(
        t("workspace.settings.profile.sole_owned_organization_item", { name: "Acme Research", count: 1 }),
        { exact: false }
      ).textContent
    ).toContain("Acme Research (1 workspace)");
    const links = screen.getAllByRole("link", { name: t("common.members_and_teams") });
    expect(links[0]?.getAttribute("href")).toBe(`/organizations/${ORG}/settings/teams`);

    // The address is typed, not suggested by a placeholder that looks filled in.
    const confirmation = screen.getByLabelText(
      t("workspace.settings.profile.please_enter_email_to_confirm_account_deletion", {
        email: "ada@example.com",
      })
    ) as HTMLInputElement;
    expect(confirmation.placeholder).toBe("");
    expect(deleteButton().disabled).toBe(true);
    fireEvent.change(confirmation, { target: { value: "ada@example.com" } });
    fireEvent.change(screen.getByLabelText(t("common.password")), { target: { value: "secret" } });
    expect(deleteButton().disabled).toBe(false);
  });

  test("says nothing more when no organization goes with the account", async () => {
    stubServer([]);
    renderModal(user("email"));
    fireEvent.change(
      await screen.findByLabelText(
        t("workspace.settings.profile.please_enter_email_to_confirm_account_deletion", {
          email: "ada@example.com",
        })
      ),
      { target: { value: "ada@example.com" } }
    );
    fireEvent.change(screen.getByLabelText(t("common.password")), { target: { value: "secret" } });
    // The deletion is offered only once the preview has loaded, and then it names nothing.
    await waitFor(() => expect(deleteButton().disabled).toBe(false));
    expect(screen.queryByRole("link", { name: t("common.members_and_teams") })).toBeNull();
  });

  test("an SSO user, confirmed only by email, is told the link cannot be sent without SMTP", async () => {
    stubServer([{ id: ORG, name: "Seed Organization", workspaceCount: 2 }], false);
    renderModal(user("sso"));

    expect(
      await screen.findByText(t("workspace.settings.profile.account_deletion_email_not_configured"))
    ).toBeTruthy();
    // The preview shows on this path too.
    expect(await screen.findByText("Seed Organization", { exact: false })).toBeTruthy();
    fireEvent.change(
      screen.getByLabelText(
        t("workspace.settings.profile.please_enter_email_to_confirm_account_deletion", {
          email: "ada@example.com",
        })
      ),
      { target: { value: "ada@example.com" } }
    );
    expect(deleteButton().disabled).toBe(true);
  });
});
