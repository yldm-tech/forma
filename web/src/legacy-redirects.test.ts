import { QueryClient } from "@tanstack/react-query";
import { createRouter } from "@tanstack/react-router";
import { afterEach, describe, expect, test, vi } from "vite-plus/test";
import { routeTree } from "./routeTree.gen";
import { Route as EnvironmentPathRoute } from "./routes/_authenticated/environments/$environmentId/$";
import { Route as EnvironmentRoute } from "./routes/_authenticated/environments/$environmentId/index";
import { Route as InternalPathRoute } from "./routes/i/$";
import { Route as ProjectOnboardingRoute } from "./routes/_authenticated/organizations/$organizationId/projects/new/$";
import { Route as ProjectRoute } from "./routes/_authenticated/projects/$projectId";
import { Route as AccountPathRoute } from "./routes/_authenticated/workspaces/$workspaceId/settings/account/$";
import { Route as AccountRoute } from "./routes/_authenticated/workspaces/$workspaceId/settings/account/index";
import { Route as OrganizationSettingsRoute } from "./routes/_authenticated/workspaces/$workspaceId/settings/organization/$";
import { Route as AppConnectionRoute } from "./routes/_authenticated/workspaces/$workspaceId/settings/workspace/app-connection";
import { Route as UserActionsRoute } from "./routes/_authenticated/workspaces/$workspaceId/settings/workspace/user-actions";
import { Route as WorkspacesRoute } from "./routes/_authenticated/workspaces/index";

// forma2's old addresses (next.config.mjs redirects, the (redirects) route handlers and the pages kept as redirects): each lands on its own route, which forwards to forma2's target, keeping the sub-path and the query.

const WS = "0190a000-0000-7000-8000-000000000002";
const ORG = "0190a000-0000-7000-8000-000000000001";

afterEach(() => vi.unstubAllGlobals());

/** The Go server answering GET paths with JSON bodies, or with an error status. */
const stubServer = (bodies: Record<string, unknown>) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      const body = bodies[path];
      if (body === undefined) throw new Error(`unexpected ${path}`);
      if (typeof body === "number")
        return new Response(JSON.stringify({ code: "forbidden", message: "Not authorized" }), {
          status: body,
          headers: { "content-type": "application/json" },
        });
      return new Response(JSON.stringify(body), { headers: { "content-type": "application/json" } });
    })
  );
};

interface HookContext {
  context: { queryClient: QueryClient };
  params: Record<string, string>;
  location: { searchStr: string };
}

/** Runs a route's beforeLoad and returns where it redirected to; a not-found is "notFound". */
const target = async (
  route: { options: { beforeLoad?: unknown } },
  params: Record<string, string>,
  searchStr = ""
): Promise<string> => {
  const beforeLoad = route.options.beforeLoad as (ctx: HookContext) => unknown;
  try {
    await beforeLoad({ context: { queryClient: new QueryClient() }, params, location: { searchStr } });
  } catch (error) {
    const thrown = error as { options?: { href?: string; to?: string }; isNotFound?: boolean };
    if (thrown.isNotFound) return "notFound";
    const href = thrown.options?.href ?? thrown.options?.to;
    if (href !== undefined) return href;
    throw error;
  }
  throw new Error("the route did not redirect");
};

test.each([
  ["/i/surveys?ref=mail", "/i/$"],
  ["/i", "/i/$"],
  [`/environments/${WS}/surveys/s1/summary`, "/_authenticated/environments/$environmentId/$"],
  [`/environments/${WS}`, "/_authenticated/environments/$environmentId/"],
  [`/organizations/${ORG}/projects/new/plan`, "/_authenticated/organizations/$organizationId/projects/new/$"],
  [`/projects/${WS}`, "/_authenticated/projects/$projectId"],
  [`/workspaces/${WS}/settings/account`, "/_authenticated/workspaces/$workspaceId/settings/account/"],
  [
    `/workspaces/${WS}/settings/account/notifications`,
    "/_authenticated/workspaces/$workspaceId/settings/account/$",
  ],
  [
    `/workspaces/${WS}/settings/organization`,
    "/_authenticated/workspaces/$workspaceId/settings/organization/$",
  ],
  [
    `/workspaces/${WS}/settings/organization/billing`,
    "/_authenticated/workspaces/$workspaceId/settings/organization/$",
  ],
  [
    `/workspaces/${WS}/settings/workspace/app-connection`,
    "/_authenticated/workspaces/$workspaceId/settings/workspace/app-connection",
  ],
  [
    `/workspaces/${WS}/settings/workspace/user-actions`,
    "/_authenticated/workspaces/$workspaceId/settings/workspace/user-actions",
  ],
  ["/workspaces", "/_authenticated/workspaces/"],
])("%s is handled by %s", (href, routeId) => {
  const router = createRouter({ routeTree, context: { queryClient: new QueryClient() } });
  const { pathname } = new URL(href, "http://localhost");
  expect(router.matchRoutes(pathname, {}).at(-1)?.routeId).toBe(routeId);
});

describe("static redirects", () => {
  test.each([
    [
      "/i/{path}",
      InternalPathRoute,
      { _splat: "workspaces/w1/surveys" },
      "?ref=mail",
      "/workspaces/w1/surveys?ref=mail",
    ],
    ["/i", InternalPathRoute, {}, "", "/"],
    [
      "/organizations/{id}/projects/new/{path}",
      ProjectOnboardingRoute,
      { organizationId: ORG, _splat: "plan" },
      "?x=1",
      `/organizations/${ORG}/workspaces/new/plan?x=1`,
    ],
    [
      "/organizations/{id}/projects/new",
      ProjectOnboardingRoute,
      { organizationId: ORG, _splat: "" },
      "",
      `/organizations/${ORG}/workspaces/new`,
    ],
    ["/projects/{id}", ProjectRoute, { projectId: WS }, "?tab=1", `/workspaces/${WS}?tab=1`],
    [
      "/workspaces/{id}/settings/account",
      AccountRoute,
      { workspaceId: WS },
      "?x=1",
      "/account/settings/profile?x=1",
    ],
    [
      "/workspaces/{id}/settings/account/{path}",
      AccountPathRoute,
      { workspaceId: WS, _splat: "notifications" },
      "?type=alert",
      "/account/settings/notifications?type=alert",
    ],
    [
      "/workspaces/{id}/settings/workspace/app-connection",
      AppConnectionRoute,
      { workspaceId: WS },
      "",
      `/workspaces/${WS}/integrations/app-connection`,
    ],
    [
      "/workspaces/{id}/settings/workspace/user-actions",
      UserActionsRoute,
      { workspaceId: WS },
      "",
      `/workspaces/${WS}/user-actions`,
    ],
    ["/workspaces", WorkspacesRoute, {}, "", "/"],
  ])("%s", async (_name, route, params, searchStr, expected) => {
    expect(await target(route, params, searchStr)).toBe(expected);
  });
});

describe("environment links", () => {
  const environment = (id: string) => `/api/app/environments/${id}`;

  test("the bare address opens the workspace's home", async () => {
    stubServer({ [environment("env1")]: { workspaceId: WS } });
    expect(await target(EnvironmentRoute, { environmentId: "env1" })).toBe(`/workspaces/${WS}/`);
  });

  test.each([
    ["surveys/s1/summary", "?referer=mail", `/workspaces/${WS}/surveys/s1/summary?referer=mail`],
    ["settings/workspace/general", "", `/workspaces/${WS}/settings/workspace/general`],
    ["project/general", "", `/workspaces/${WS}/workspace/general`],
    ["project", "", `/workspaces/${WS}/workspace`],
    ["projects", "", `/workspaces/${WS}/projects`],
  ])("a sub-path %s keeps its place in the workspace", async (splat, searchStr, expected) => {
    stubServer({ [environment("env1")]: { workspaceId: WS } });
    expect(await target(EnvironmentPathRoute, { environmentId: "env1", _splat: splat }, searchStr)).toBe(
      expected
    );
  });

  test("an id the user may not navigate is not found", async () => {
    stubServer({ [environment("env1")]: 404 });
    expect(await target(EnvironmentPathRoute, { environmentId: "env1", _splat: "surveys" })).toBe("notFound");
  });
});

describe("organization settings under a workspace", () => {
  const workspace = { id: WS, organizationId: ORG, name: "W" };

  test.each([
    ["", "", `/organizations/${ORG}/settings/`],
    ["billing", "", `/organizations/${ORG}/settings/billing`],
    ["billing", "?checkout_success=1", `/organizations/${ORG}/settings/billing?checkout_success=1`],
    ["teams", "", `/organizations/${ORG}/settings/teams`],
  ])("/%s%s moves to the organization's settings", async (splat, searchStr, expected) => {
    stubServer({ [`/api/app/workspaces/${WS}`]: workspace });
    expect(await target(OrganizationSettingsRoute, { workspaceId: WS, _splat: splat }, searchStr)).toBe(
      expected
    );
  });

  test("a workspace the user may not open reveals no organization", async () => {
    stubServer({ [`/api/app/workspaces/${WS}`]: 403 });
    expect(await target(OrganizationSettingsRoute, { workspaceId: WS, _splat: "billing" })).toBe("notFound");
  });
});
