import { QueryClient } from "@tanstack/react-query";
import { afterEach, describe, expect, test, vi } from "vite-plus/test";
import { Route as SetupRoute } from "./routes/setup/index";
import { Route as SetupSignupRoute } from "./routes/setup/signup";

// forma2's /setup (app/setup/page.tsx) and its fresh-instance sign-up (modules/setup/(fresh-instance)): both follow the root page's answer, GET /api/app/home.

afterEach(() => vi.unstubAllGlobals());

const stubHome = (home: { path?: string; workspaceIds?: string[] }) =>
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      if (path !== "/api/app/home") throw new Error(`unexpected ${path}`);
      return new Response(JSON.stringify({ workspaceIds: [], ...home }), {
        headers: { "content-type": "application/json" },
      });
    })
  );

/** Runs a route's beforeLoad and returns where it redirected to, or null when it let the page load. */
const visit = async (route: { options: { beforeLoad?: unknown } }): Promise<string | null> => {
  const beforeLoad = route.options.beforeLoad as (ctx: { context: { queryClient: QueryClient } }) => unknown;
  try {
    await beforeLoad({ context: { queryClient: new QueryClient() } });
    return null;
  } catch (error) {
    const options = (error as { options?: { href?: string; to?: string } }).options;
    const href = options?.href ?? options?.to;
    if (href === undefined) throw error;
    return href;
  }
};

describe("/setup", () => {
  test.each([
    ["a fresh instance goes to the intro", { path: "/setup/intro" }, "/setup/intro"],
    [
      "a signed-in user without an organization creates one",
      { path: "/setup/organization/create" },
      "/setup/organization/create",
    ],
    ["a signed-out visitor of an instance with users goes to the root page", { path: "/auth/login" }, "/"],
    ["anyone else goes to the root page", { workspaceIds: ["w1"] }, "/"],
    ["an onboarding answer is the root page's to give", { path: "/organizations/o1/landing" }, "/"],
  ])("%s", async (_name, home, expected) => {
    stubHome(home);
    expect(await visit(SetupRoute)).toBe(expected);
  });
});

describe("/setup/signup", () => {
  test("is the first administrator's sign-up while the instance is fresh", async () => {
    stubHome({ path: "/setup/intro" });
    expect(await visit(SetupSignupRoute)).toBeNull();
  });

  test.each([
    ["once the instance has a user", { path: "/auth/login" }],
    ["for a signed-in visitor", { workspaceIds: ["w1"] }],
  ])("leaves the decision to the root page %s", async (_name, home) => {
    stubHome(home);
    expect(await visit(SetupSignupRoute)).toBe("/");
  });
});
