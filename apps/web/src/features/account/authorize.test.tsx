import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import type { AuthorizationRequest } from "../../lib/queries/oauth";
import { toast } from "../../lib/toast";
import { AuthorizePage } from "./authorize";

// forma2's /account/authorize page and its OAuthConsentActions, against the Go server's /api/app/oauth endpoints (apps/server/internal/oauthserver/app.go).

const REQUEST_ID = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0f";
const PATH = `/api/app/oauth/authorization-requests/${REQUEST_ID}`;

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
  vi.unstubAllGlobals();
});

const pending = (overrides: Partial<AuthorizationRequest> = {}): AuthorizationRequest => ({
  requestId: REQUEST_ID,
  client: { clientId: "c1", name: "Claude", uri: "https://claude.ai", logoUri: null },
  redirectUri: "https://claude.ai/api/mcp/auth_callback",
  scopes: ["openid", "surveys:read", "workflows:write"],
  resource: "https://forma.example.com/api/mcp",
  consentRequired: true,
  ...overrides,
});

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

/** The server: the pending request (or a 404), and the consent answer forma2's Better Auth endpoint gave. */
const stubServer = (request: AuthorizationRequest | null, answer: (accept: boolean) => Response) => {
  const answers: unknown[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init: RequestInit = {}) => {
      if (path === PATH && init.method === "GET") {
        return request
          ? json(200, request)
          : json(404, { code: "not_found", message: "Authorization request not found" });
      }
      if (path === `${PATH}/consent` && init.method === "POST") {
        const body = JSON.parse(init.body as string) as { accept: boolean };
        answers.push(body);
        return answer(body.accept);
      }
      return json(404, { code: "not_found", message: path });
    })
  );
  return answers;
};

/** Records where the page sends the browser. */
const stubLocation = () => {
  const location = { ...globalThis.location, href: "http://localhost:3000/account/authorize" };
  vi.stubGlobal("location", location);
  return location;
};

const renderPage = (requestId?: string) => {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <AuthorizePage requestId={requestId} />
      </QueryClientProvider>
    </I18nextProvider>
  );
};

describe("AuthorizePage", () => {
  test("shows the client, where it returns to, its site and the permissions it asks for", async () => {
    stubServer(pending(), () => json(500, {}));
    renderPage(REQUEST_ID);

    expect(
      await screen.findByRole("heading", {
        name: i18n.t("auth.oauth.authorize_title", { clientName: "Claude" }),
      })
    ).toBeTruthy();
    expect(screen.getByText("https://claude.ai")).toBeTruthy();
    expect(screen.getByText("claude.ai")).toBeTruthy();
    expect(screen.getByText(i18n.t("auth.oauth.scopes.openid"))).toBeTruthy();
    expect(screen.getByText(i18n.t("auth.oauth.scopes.surveys_read"))).toBeTruthy();
    expect(screen.getByText(i18n.t("auth.oauth.scopes.workflows_write"))).toBeTruthy();
    expect(screen.queryByText(i18n.t("auth.oauth.localhost_redirect_warning"))).toBeNull();
  });

  test("warns when the client returns to this machine, and names an unnamed client", async () => {
    stubServer(
      pending({
        client: { clientId: "c2", name: "", uri: null, logoUri: null },
        redirectUri: "http://127.0.0.1:6274/oauth/callback",
      }),
      () => json(500, {})
    );
    renderPage(REQUEST_ID);

    expect(await screen.findByText(i18n.t("auth.oauth.localhost_redirect_warning"))).toBeTruthy();
    expect(screen.getByText("127.0.0.1:6274")).toBeTruthy();
    expect(
      screen.getByRole("heading", {
        name: i18n.t("auth.oauth.authorize_title", { clientName: i18n.t("auth.oauth.unknown_client") }),
      })
    ).toBeTruthy();
  });

  test("an unknown or already answered request is an invalid OAuth request", async () => {
    stubServer(null, () => json(500, {}));
    renderPage(REQUEST_ID);

    expect(await screen.findByText(i18n.t("auth.oauth.invalid_oauth_request"))).toBeTruthy();
    expect(screen.queryByRole("button", { name: i18n.t("auth.oauth.allow_access") })).toBeNull();
  });

  test("a page opened without a request is an invalid OAuth request", () => {
    stubServer(pending(), () => json(500, {}));
    renderPage(undefined);

    expect(screen.getByText(i18n.t("auth.oauth.invalid_oauth_request"))).toBeTruthy();
  });

  test("Allow approves the request and sends the browser back to the client with its code", async () => {
    const location = stubLocation();
    const answers = stubServer(pending(), () =>
      json(200, { redirect: true, url: "https://forma.example.com/api/auth/oauth2/authorize/callback?id=x" })
    );
    renderPage(REQUEST_ID);

    fireEvent.click(await screen.findByRole("button", { name: i18n.t("auth.oauth.allow_access") }));

    await waitFor(() =>
      expect(location.href).toBe("https://forma.example.com/api/auth/oauth2/authorize/callback?id=x")
    );
    expect(answers).toEqual([{ accept: true }]);
  });

  test("Deny refuses the request and sends the browser back to the client with access_denied", async () => {
    const location = stubLocation();
    const answers = stubServer(pending(), () =>
      json(200, { redirect: true, url: "https://claude.ai/api/mcp/auth_callback?error=access_denied" })
    );
    renderPage(REQUEST_ID);

    fireEvent.click(await screen.findByRole("button", { name: i18n.t("auth.oauth.deny_access") }));

    await waitFor(() =>
      expect(location.href).toBe("https://claude.ai/api/mcp/auth_callback?error=access_denied")
    );
    expect(answers).toEqual([{ accept: false }]);
  });

  test("a client the user already approved continues without asking again", async () => {
    const location = stubLocation();
    const answers = stubServer(pending({ consentRequired: false }), () =>
      json(200, { redirect: true, url: "https://forma.example.com/api/auth/oauth2/authorize/callback?id=x" })
    );
    renderPage(REQUEST_ID);

    await waitFor(() =>
      expect(location.href).toBe("https://forma.example.com/api/auth/oauth2/authorize/callback?id=x")
    );
    expect(answers).toEqual([{ accept: true }]);
    expect(screen.queryByRole("button", { name: i18n.t("auth.oauth.allow_access") })).toBeNull();
  });

  test("when continuing on its own fails, the page asks instead of waiting forever", async () => {
    const location = stubLocation();
    const answers = stubServer(pending({ consentRequired: false }), () =>
      json(500, { code: "internal_server_error", message: "boom" })
    );
    renderPage(REQUEST_ID);

    const allow = await screen.findByRole("button", { name: i18n.t("auth.oauth.allow_access") });
    expect(toast.getSnapshot().map((shown) => shown.message)).toContain(i18n.t("auth.oauth.consent_failed"));
    expect(answers).toEqual([{ accept: true }]);
    expect(location.href).toBe("http://localhost:3000/account/authorize");
    // The user can try again.
    fireEvent.click(allow);
    await waitFor(() => expect(answers).toEqual([{ accept: true }, { accept: true }]));
  });

  test("when the request expired or was answered elsewhere before continuing, the page says it is invalid", async () => {
    stubServer(pending({ consentRequired: false }), () =>
      json(404, { code: "not_found", message: "Authorization request not found" })
    );
    renderPage(REQUEST_ID);

    expect(await screen.findByText(i18n.t("auth.oauth.invalid_oauth_request"))).toBeTruthy();
    expect(screen.queryByRole("button", { name: i18n.t("auth.oauth.allow_access") })).toBeNull();
  });

  test("a redirect that would run script in this page is refused with forma2's toast", async () => {
    const location = stubLocation();
    stubServer(pending(), () => json(200, { redirect: true, url: "javascript:alert(1)" }));
    renderPage(REQUEST_ID);

    fireEvent.click(await screen.findByRole("button", { name: i18n.t("auth.oauth.allow_access") }));

    await waitFor(() =>
      expect(toast.getSnapshot().map((shown) => shown.message)).toContain(i18n.t("auth.oauth.consent_failed"))
    );
    expect(location.href).toBe("http://localhost:3000/account/authorize");
  });

  test("a failed answer shows forma2's toast and keeps the page", async () => {
    stubServer(pending(), () => json(500, { code: "internal_server_error", message: "boom" }));
    renderPage(REQUEST_ID);

    fireEvent.click(await screen.findByRole("button", { name: i18n.t("auth.oauth.allow_access") }));

    await waitFor(() =>
      expect(toast.getSnapshot().map((shown) => shown.message)).toContain(i18n.t("auth.oauth.consent_failed"))
    );
    expect(screen.getByRole("button", { name: i18n.t("auth.oauth.allow_access") })).toBeTruthy();
  });
});
