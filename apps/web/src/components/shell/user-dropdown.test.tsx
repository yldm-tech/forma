import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { useState } from "react";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import type { Me } from "../../lib/queries/auth";
import { UnsavedChangesGuard, useUnsavedChanges } from "./unsaved-changes";
import { UserDropdown } from "./user-dropdown";

let i18n: I18n;
const t = (key: string) => i18n.t(key);

beforeAll(async () => {
  i18n = await initI18n("en-US");
  // jsdom has the <dialog> element but not its modal methods; these stand in for what the Dialog reads of them.
  HTMLDialogElement.prototype.showModal ??= function (this: HTMLDialogElement) {
    this.setAttribute("open", "");
  };
  HTMLDialogElement.prototype.close ??= function (this: HTMLDialogElement) {
    this.removeAttribute("open");
  };
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const user: Me = {
  id: "u1",
  name: "Ada Owner",
  email: "ada@example.com",
  emailVerified: true,
  locale: "en-US",
  identityProvider: "email",
  twoFactorEnabled: false,
  createdAt: "2026-01-01T00:00:00Z",
  lastLoginAt: null,
};

/** A settings form: dirty while its field differs from the stored "Acme". */
const NameForm = () => {
  const [name, setName] = useState("Acme");
  useUnsavedChanges(name !== "Acme");
  return (
    <label>
      Workspace name
      <input value={name} onChange={(event) => setName(event.target.value)} />
    </label>
  );
};

/** The app shell's layout: the top bar's menu sits outside the guard that wraps the page. */
const renderShell = async () => {
  const rootRoute = createRootRoute({
    component: () => (
      <>
        <UserDropdown user={user} organizationId="org1" isBilling={false} />
        <UnsavedChangesGuard>
          <Outlet />
        </UnsavedChangesGuard>
      </>
    ),
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({ getParentRoute: () => rootRoute, path: "/", component: NameForm }),
      createRoute({
        getParentRoute: () => rootRoute,
        path: "/auth/login",
        component: () => <p>login page</p>,
      }),
    ]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={new QueryClient()}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
  await router.load();
  return router;
};

// The session ends before the page changes, so a "Discard changes?" question would offer to keep editing a form that can no longer be saved.
test("logging out with unsaved edits goes to the sign-in page without asking to keep editing", async () => {
  const fetch = vi.fn(
    async () => new Response("{}", { status: 200, headers: { "content-type": "application/json" } })
  );
  vi.stubGlobal("fetch", fetch);
  const router = await renderShell();
  fireEvent.change(await screen.findByLabelText("Workspace name"), { target: { value: "Acme Labs" } });

  fireEvent.keyDown(screen.getByRole("button", { name: t("common.account_settings") }), { key: "Enter" });
  fireEvent.click(await screen.findByRole("menuitem", { name: t("common.logout") }));

  await waitFor(() => expect(router.state.location.pathname).toBe("/auth/login"));
  await act(async () => {});
  expect(screen.getByText("login page")).toBeTruthy();
  expect(
    screen.queryByText(t("common.discard_changes_title"))?.closest("dialog")?.hasAttribute("open") ?? false
  ).toBe(false);
  expect(fetch).toHaveBeenCalledWith("/api/app/auth/sign-out", expect.anything());
});
