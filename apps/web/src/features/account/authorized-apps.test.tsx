import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import type { OAuthConsent } from "../../lib/queries/oauth";
import { toast } from "../../lib/toast";
import { AuthorizedAppsCard } from "./authorized-apps";

// forma2's AuthorizedAppsCard and RevokeOAuthConsentButton on the profile page, against GET and DELETE /api/app/oauth/consents (apps/server/internal/oauthserver/app.go).

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
  vi.unstubAllGlobals();
});

const consent = (overrides: Partial<OAuthConsent> = {}): OAuthConsent => ({
  id: "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d01",
  client: { clientId: "c1", name: "Claude", uri: "https://claude.ai/connectors", logoUri: null },
  scopes: ["surveys:read", "offline_access"],
  createdAt: "2026-10-01T09:30:00Z",
  updatedAt: "2026-10-02T10:45:00Z",
  ...overrides,
});

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

/** The server's consents; a DELETE removes one (204) or fails with the given answer. */
const stubServer = (consents: OAuthConsent[], failDelete?: Response) => {
  let current = consents;
  const deleted: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      if (path === "/api/app/oauth/consents" && init.method === "GET") return json(200, current);
      const id = path.startsWith("/api/app/oauth/consents/") ? path.split("/").pop() : undefined;
      if (id && init.method === "DELETE") {
        if (failDelete) return failDelete;
        deleted.push(id);
        current = current.filter((c) => c.id !== id);
        return new Response(null, { status: 204 });
      }
      return json(404, { code: "not_found", message: path });
    })
  );
  return deleted;
};

const renderCard = () => {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <AuthorizedAppsCard locale="en-US" />
      </QueryClientProvider>
    </I18nextProvider>
  );
};

describe("AuthorizedAppsCard", () => {
  test("says no client is connected yet", async () => {
    stubServer([]);
    renderCard();

    expect(await screen.findByText(i18n.t("auth.oauth.no_authorized_apps"))).toBeTruthy();
  });

  test("lists each connected client with its host and the permissions it holds", async () => {
    stubServer([
      consent(),
      consent({
        id: "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d02",
        client: { clientId: "client-without-metadata", name: "", uri: null, logoUri: null },
        scopes: ["workflows:write"],
      }),
    ]);
    renderCard();

    const rows = (await screen.findAllByRole("row")).slice(1);
    expect(rows).toHaveLength(2);
    const [claude, unnamed] = rows as [HTMLElement, HTMLElement];
    expect(within(claude).getByText("Claude")).toBeTruthy();
    expect(within(claude).getByText("claude.ai")).toBeTruthy();
    expect(within(claude).getByText(i18n.t("auth.oauth.scopes.surveys_read"))).toBeTruthy();
    expect(within(claude).getByText(i18n.t("auth.oauth.scopes.offline_access"))).toBeTruthy();
    // A client without a name or a site is shown by its id.
    expect(within(unnamed).getAllByText("client-without-metadata")).toHaveLength(2);
    expect(within(unnamed).getByText(i18n.t("auth.oauth.scopes.workflows_write"))).toBeTruthy();
  });

  test("Revoke asks first, then removes the client from the list", async () => {
    const confirm = vi.fn(() => true);
    vi.stubGlobal("confirm", confirm);
    const deleted = stubServer([consent()]);
    renderCard();

    fireEvent.click(await screen.findByRole("button", { name: i18n.t("auth.oauth.revoke") }));

    await waitFor(() => expect(deleted).toEqual(["0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d01"]));
    expect(confirm).toHaveBeenCalledWith(i18n.t("auth.oauth.revoke_confirmation"));
    expect(await screen.findByText(i18n.t("auth.oauth.no_authorized_apps"))).toBeTruthy();
    expect(toast.getSnapshot().map((shown) => shown.message)).toContain(i18n.t("auth.oauth.consent_revoked"));
  });

  test("a declined confirmation keeps the client", async () => {
    vi.stubGlobal(
      "confirm",
      vi.fn(() => false)
    );
    const deleted = stubServer([consent()]);
    renderCard();

    fireEvent.click(await screen.findByRole("button", { name: i18n.t("auth.oauth.revoke") }));

    expect(deleted).toEqual([]);
    expect(screen.getByText("Claude")).toBeTruthy();
  });

  test("a failed revocation shows the server's message", async () => {
    vi.stubGlobal(
      "confirm",
      vi.fn(() => true)
    );
    stubServer([consent()], json(404, { code: "not_found", message: "Consent not found" }));
    renderCard();

    fireEvent.click(await screen.findByRole("button", { name: i18n.t("auth.oauth.revoke") }));

    await waitFor(() => expect(toast.getSnapshot().length).toBe(1));
    expect(screen.getByText("Claude")).toBeTruthy();
  });
});
