import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider, createMemoryHistory, createRouter } from "@tanstack/react-router";
import { cleanup, render, screen } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../lib/i18n";
import { routeTree } from "../routeTree.gen";

// Every state of the invite page says what the link is and leaves the visitor a way on.

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const USER = { id: "u1", name: "Ada", email: "ada@example.com", locale: "en-US" };

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

/** The session and the inspection answers of the server. */
const renderInvite = async (signedIn: boolean, inspection: Record<string, unknown>) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      if (path === "/api/app/auth/me") {
        return signedIn ? json(200, USER) : json(401, { code: "not_authenticated", message: "x" });
      }
      if (path === "/api/app/invites/inspect") return json(200, inspection);
      throw new Error(`unexpected ${path}`);
    })
  );
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createRouter({
    routeTree,
    context: { queryClient },
    history: createMemoryHistory({ initialEntries: ["/invite?token=tok"] }),
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

const link = (name: string) => screen.findByRole("link", { name });

describe("the invite page", () => {
  test("names who invited the visitor to which organization, and offers sign-up and sign-in", async () => {
    await renderInvite(false, {
      status: "valid",
      email: "grace@example.com",
      organizationName: "Acme",
      inviterName: "Olivia",
    });
    expect(
      await screen.findByText(
        i18n.t("auth.invite.invited_you_to_join", { inviterName: "Olivia", organizationName: "Acme" })
      )
    ).toBeTruthy();
    expect((await link(i18n.t("auth.invite.create_account"))).getAttribute("href")).toContain(
      "inviteToken=tok"
    );
    expect((await link(i18n.t("auth.invite.login"))).getAttribute("href")).toContain("callbackUrl=");
  });

  test("tells a signed-in visitor whose invite is used up that they may already be a member", async () => {
    await renderInvite(true, { status: "not_found" });
    expect(await screen.findByText(i18n.t("auth.invite.already_used_signed_in"))).toBeTruthy();
    expect((await link(i18n.t("auth.invite.go_to_app"))).getAttribute("href")).toBe("/");
  });

  test("offers sign-in for an unknown link when signed out", async () => {
    await renderInvite(false, { status: "not_found" });
    expect(await screen.findByText(i18n.t("auth.invite.invite_not_found"))).toBeTruthy();
    expect(screen.queryByText(i18n.t("auth.invite.already_used_signed_in"))).toBeNull();
    expect((await link(i18n.t("auth.invite.login"))).getAttribute("href")).toBe("/auth/login");
  });

  test("leaves a way on from an expired invite", async () => {
    await renderInvite(true, { status: "expired" });
    expect(await screen.findByText(i18n.t("auth.invite.invite_expired"))).toBeTruthy();
    expect(await link(i18n.t("auth.invite.go_to_app"))).toBeTruthy();
  });

  test("names the invited address on a mismatch and offers to switch account", async () => {
    await renderInvite(true, {
      status: "email_mismatch",
      email: "grace@example.com",
      organizationName: "Acme",
    });
    expect(
      await screen.findByText(
        i18n.t("auth.invite.email_does_not_match_for", {
          email: "grace@example.com",
          currentEmail: USER.email,
        })
      )
    ).toBeTruthy();
    expect(screen.getByRole("button", { name: i18n.t("auth.invite.switch_account") })).toBeTruthy();
    expect(await link(i18n.t("auth.invite.go_to_app"))).toBeTruthy();
  });
});
