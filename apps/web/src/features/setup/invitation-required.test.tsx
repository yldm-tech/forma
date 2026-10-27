import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider, createMemoryHistory, createRouter } from "@tanstack/react-router";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import { routeTree } from "../../routeTree.gen";

// The page the root page sends a user without an organization to: the step that creates one where they may, and on an invite-only instance, where only the owner of an organization may create another, a notice to ask an owner for an invitation, with a way to sign out.

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

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

/** Opens the page as a signed-in user the server does or does not let create an organization; returns the requests sent and the router. */
const renderPage = async (canCreate: boolean) => {
  const requests: string[] = [];
  let signedIn = true;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      const key = `${init.method ?? "GET"} ${path}`;
      requests.push(key);
      if (key === "GET /api/app/auth/me") {
        return signedIn
          ? json({ id: "u1", name: "Ada", email: "ada@example.com", locale: "en-US" })
          : json({ code: "not_authenticated", message: "Not authenticated" }, 401);
      }
      if (key === "GET /api/app/organization-creation") return json({ allowed: canCreate });
      if (key === "POST /api/app/auth/sign-out") {
        signedIn = false;
        return new Response(null, { status: 204 });
      }
      if (key === "GET /api/app/auth/options") {
        return json({ emailAuthEnabled: true, signupEnabled: false, ssoProviders: [] });
      }
      throw new Error(`unexpected ${key}`);
    })
  );
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createRouter({
    routeTree,
    context: { queryClient },
    history: createMemoryHistory({ initialEntries: ["/setup/organization/create"] }),
  });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  );
  await router.load();
  return { requests, router };
};

describe("/setup/organization/create", () => {
  test("someone who may create an organization gets the step that creates one", async () => {
    await renderPage(true);
    expect(await screen.findByLabelText(t("workspace.settings.general.organization_name"))).toBeTruthy();
    expect(screen.queryByText(t("organization_creation.invitation_required_title"))).toBeNull();
  });

  test("on an invite-only instance, someone without an organization is told to ask for an invitation and can sign out", async () => {
    const { requests, router } = await renderPage(false);
    expect(await screen.findByText(t("organization_creation.invitation_required_title"))).toBeTruthy();
    expect(screen.getByText(t("organization_creation.invitation_required_description"))).toBeTruthy();
    expect(screen.queryByLabelText(t("workspace.settings.general.organization_name"))).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: t("common.logout") }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/auth/login"));
    expect(requests).toContain("POST /api/app/auth/sign-out");
    expect(requests).not.toContain("POST /api/app/organizations");
  });
});
