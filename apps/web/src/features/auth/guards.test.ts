import { QueryClient } from "@tanstack/react-query";
import { isRedirect } from "@tanstack/react-router";
import { afterEach, beforeEach, describe, expect, test, vi } from "vite-plus/test";
import { ApiError } from "../../lib/api";
import { redirectIfSignedIn, signedInRedirectTarget, signupAvailable } from "./guards";

const ORIGIN = "http://localhost:3000";

const user = {
  id: "0190f1a0-0000-7000-8000-000000000001",
  name: "Ada",
  email: "ada@example.com",
  emailVerified: true,
  locale: "en-US",
  identityProvider: "email",
  twoFactorEnabled: false,
  createdAt: "2026-01-01T00:00:00Z",
  lastLoginAt: null,
};

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

// The server, answering by path.
const serve = (routes: Record<string, () => Response>) => {
  const fetchMock = vi.fn(async (path: string) => {
    const route = routes[path];
    if (!route) throw new Error(`unexpected request to ${path}`);
    return route();
  });
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
};

const signedIn = () => serve({ "/api/app/auth/me": () => json(200, user) });
const signedOut = () =>
  serve({ "/api/app/auth/me": () => json(401, { code: "not_authenticated", message: "Not authenticated" }) });

let queryClient: QueryClient;

beforeEach(() => {
  // The app's freshness window (createQueryClient), without its retries of server failures.
  queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: 30_000 } } });
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("signedInRedirectTarget", () => {
  test("lets a visitor without a session stay", async () => {
    signedOut();
    await expect(signedInRedirectTarget(queryClient, "/invite?token=abc", ORIGIN)).resolves.toBeNull();
  });

  test("sends a signed-in visitor to the callback's path", async () => {
    signedIn();
    await expect(signedInRedirectTarget(queryClient, `${ORIGIN}/invite?token=abc`, ORIGIN)).resolves.toBe(
      "/invite?token=abc"
    );
  });

  test("sends a signed-in visitor to the root page without a callback or with one that fails validation", async () => {
    signedIn();
    await expect(signedInRedirectTarget(queryClient, undefined, ORIGIN)).resolves.toBe("/");
    await expect(signedInRedirectTarget(queryClient, "https://evil.example/x", ORIGIN)).resolves.toBe("/");
    await expect(signedInRedirectTarget(queryClient, `${ORIGIN}//evil.example/x`, ORIGIN)).resolves.toBe("/");
  });

  test("asks the server once while the answer is fresh", async () => {
    const fetchMock = signedOut();
    await signedInRedirectTarget(queryClient, undefined, ORIGIN);
    await signedInRedirectTarget(queryClient, undefined, ORIGIN);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  test("does not mistake a server failure for a signed-out visitor", async () => {
    serve({ "/api/app/auth/me": () => json(500, { code: "internal_server_error", message: "x" }) });
    await expect(signedInRedirectTarget(queryClient, undefined, ORIGIN)).rejects.toBeInstanceOf(ApiError);
  });
});

describe("redirectIfSignedIn", () => {
  beforeEach(() => {
    vi.stubGlobal("window", { location: { origin: ORIGIN } });
  });

  const run = (search: object) => redirectIfSignedIn({ context: { queryClient }, search });

  test("throws a redirect to the callback for a signed-in visitor", async () => {
    signedIn();
    const thrown: unknown = await run({ callbackUrl: `${ORIGIN}/workspaces/1` }).catch(
      (error: unknown) => error
    );
    expect(isRedirect(thrown)).toBe(true);
    expect((thrown as { options: { href: string; replace: boolean } }).options).toMatchObject({
      href: "/workspaces/1",
      replace: true,
    });
  });

  test("redirects to the root page from a screen without a callback", async () => {
    signedIn();
    const thrown: unknown = await run({ inviteToken: "abc" }).catch((error: unknown) => error);
    expect(isRedirect(thrown)).toBe(true);
    expect((thrown as { options: { href: string } }).options.href).toBe("/");
  });

  test("lets a signed-out visitor through", async () => {
    signedOut();
    await expect(run({ callbackUrl: "/workspaces/1" })).resolves.toBeUndefined();
  });
});

describe("signupAvailable", () => {
  test("is open when the instance takes uninvited sign-ups, without asking about an invite", async () => {
    const fetchMock = serve({});
    await expect(signupAvailable(true, undefined)).resolves.toBe(true);
    await expect(signupAvailable(true, "abc")).resolves.toBe(true);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  test("is closed without an invite when sign-up is off", async () => {
    await expect(signupAvailable(false, undefined)).resolves.toBe(false);
  });

  test.each([
    ["valid", true],
    ["expired", false],
    ["not_found", false],
  ])("with sign-up off, an invite that inspects as %s opens it: %s", async (status, expected) => {
    const fetchMock = serve({ "/api/app/invites/inspect": () => json(200, { status }) });
    await expect(signupAvailable(false, "abc")).resolves.toBe(expected);
    const [, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(init.method).toBe("POST");
    expect(init.body).toBe(JSON.stringify({ token: "abc" }));
  });

  test("treats a link the server cannot classify as no invite", async () => {
    serve({
      "/api/app/invites/inspect": () =>
        json(400, { code: "validation_error", message: "Invalid input", details: { fields: [] } }),
    });
    await expect(signupAvailable(false, "x".repeat(600))).resolves.toBe(false);
  });
});
