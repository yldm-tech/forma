import { QueryClient } from "@tanstack/react-query";
import { createRouter } from "@tanstack/react-router";
import { afterEach, describe, expect, test, vi } from "vite-plus/test";
import { routeTree } from "./routeTree.gen";
import { Route as OrganizationSettingsRoute } from "./routes/_authenticated/organizations/$organizationId/settings";
import { Route as PlanStepRoute } from "./routes/_authenticated/organizations/$organizationId/workspaces/new/plan";
import { Route as LegacyIntegrationRoute } from "./routes/_authenticated/workspaces/$workspaceId/settings/workspace/integrations/$";

// Pages the Go server sends the browser to: redirects (the root page's GET /api/app/home, SSO callbacks, workspace and organization navigation) and links in the mails it sends. Each must be a page of the SPA, not its not-found screen.
const SERVER_PAGES = [
  "/setup/intro",
  "/setup/organization/create",
  "/auth/login",
  "/auth/signup?error=email_domain_not_allowed",
  "/auth/sso-recovery?token=t",
  "/auth/verify-email?token=t",
  "/auth/verification-requested?purpose=sso_recovery",
  "/auth/forgot-password/reset?token=t",
  "/auth/delete-account?token=t",
  "/verify-email-change?token=t",
  "/invite?token=t",
  "/account/settings/profile",
  "/organizations/0190a000-0000-7000-8000-000000000001/landing",
  "/organizations/0190a000-0000-7000-8000-000000000001/workspaces/new/plan",
  "/organizations/0190a000-0000-7000-8000-000000000001/workspaces/new/survey",
  // Billing: the billing role's home on Forma Cloud and Stripe's return URLs (apps/server/internal/billing/plans.go).
  "/organizations/0190a000-0000-7000-8000-000000000001/settings/billing",
  "/organizations/0190a000-0000-7000-8000-000000000001/settings/billing?checkout_success=1&upgrade_pending=1&session_id=cs_1",
  "/billing-confirmation?organizationId=0190a000-0000-7000-8000-000000000001&checkout_success=1&plan=pro",
  "/workspaces/0190a000-0000-7000-8000-000000000002/",
  // The integrations' OAuth callbacks (apps/server/internal/integration/connect.go callbackPaths).
  "/workspaces/0190a000-0000-7000-8000-000000000002/integrations/google-sheets?error=access_denied",
  "/workspaces/0190a000-0000-7000-8000-000000000002/settings/workspace/integrations/airtable",
  "/workspaces/0190a000-0000-7000-8000-000000000002/settings/workspace/integrations/notion?error=oauth_error",
  "/workspaces/0190a000-0000-7000-8000-000000000002/settings/workspace/integrations/slack",
];

test.each(SERVER_PAGES)("%s is a page of the SPA", (href) => {
  const router = createRouter({ routeTree, context: { queryClient: new QueryClient() } });
  const { pathname } = new URL(href, "http://localhost");
  const matches = router.matchRoutes(pathname, {});
  const leaf = matches.at(-1);
  // A path no page matches ends at the root or at a layout (/auth, /_authenticated), which have children; a page is a route without any.
  const route = leaf ? router.routesById[leaf.routeId as keyof typeof router.routesById] : undefined;
  expect(leaf?.routeId).not.toBe("__root__");
  expect(route?.children).toBeUndefined();
});

// forma2 rendered the templates page in the survey editor's full-screen layout, outside the workspace's navigation, while the survey list sits inside it.
test.each([
  [
    "/workspaces/0190a000-0000-7000-8000-000000000002/surveys/templates",
    "/_authenticated/workspaces/$workspaceId_/surveys/templates",
  ],
  [
    "/workspaces/0190a000-0000-7000-8000-000000000002/surveys",
    "/_authenticated/workspaces/$workspaceId/surveys/",
  ],
])("%s is %s", (pathname, routeId) => {
  const router = createRouter({ routeTree, context: { queryClient: new QueryClient() } });
  expect(router.matchRoutes(pathname, {}).at(-1)?.routeId).toBe(routeId);
});

// The Airtable, Notion and Slack callbacks land on the old settings address; the page they forward to reads the provider's ?error.
test("the old address of an integration page forwards with its query", () => {
  const beforeLoad = LegacyIntegrationRoute.options.beforeLoad as unknown as (ctx: {
    params: { workspaceId: string; _splat?: string };
    location: { searchStr: string };
  }) => void;
  let thrown: unknown;
  try {
    beforeLoad({
      params: { workspaceId: "w1", _splat: "notion" },
      location: { searchStr: "?error=access_denied" },
    });
  } catch (error) {
    thrown = error;
  }
  expect((thrown as { options?: { href?: string } }).options?.href).toBe(
    "/workspaces/w1/integrations/notion?error=access_denied"
  );
});

const ORG = "0190a000-0000-7000-8000-000000000001";

afterEach(() => vi.unstubAllGlobals());

/** The Go server answering GET paths with JSON bodies. */
const stubServer = (bodies: Record<string, unknown>) => {
  const requested: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      requested.push(path);
      if (!(path in bodies)) throw new Error(`unexpected ${path}`);
      return new Response(JSON.stringify(bodies[path]), { headers: { "content-type": "application/json" } });
    })
  );
  return requested;
};

/** Runs a route hook and returns where it redirected to, or null when it let the page load. */
const redirectOf = async (run: () => unknown): Promise<string | null> => {
  try {
    await run();
    return null;
  } catch (error) {
    const href = (error as { options?: { href?: string } }).options?.href;
    if (href === undefined) throw error;
    return href;
  }
};

describe("the onboarding plan step", () => {
  const beforeLoad = PlanStepRoute.options.beforeLoad as unknown as (ctx: {
    context: { queryClient: QueryClient; onboarding: { workspaceId: string; completedPath: string | null } };
    params: { organizationId: string };
  }) => Promise<void>;

  const summary = (isFormaCloud: boolean, plan: string) => ({
    isFormaCloud,
    plan,
    subscriptionStatus: null,
    trialEnd: null,
    trialDaysRemaining: null,
    proTrialDays: 14,
    limits: { workspaces: 1, monthly: { responses: 250 } },
    usage: null,
    usageCycle: null,
  });

  const visit = (completedPath: string | null = null) =>
    redirectOf(() =>
      beforeLoad({
        context: { queryClient: new QueryClient(), onboarding: { workspaceId: "w1", completedPath } },
        params: { organizationId: ORG },
      })
    );

  test.each([
    ["off Forma Cloud", false, "hobby", null, `/organizations/${ORG}/workspaces/new/survey`],
    [
      "off Forma Cloud before anything else",
      false,
      "hobby",
      "/workspaces/w1/",
      `/organizations/${ORG}/workspaces/new/survey`,
    ],
    ["for a workspace that already has a survey", true, "hobby", "/workspaces/w1/", "/workspaces/w1/"],
    ["for a paying organization", true, "pro", null, `/organizations/${ORG}/workspaces/new/survey`],
    ["for a custom contract", true, "custom", null, `/organizations/${ORG}/workspaces/new/survey`],
    ["for Hobby on Forma Cloud, the step itself", true, "hobby", null, null],
    ["for an organization nothing was synced for yet, the step itself", true, "unknown", null, null],
  ])("%s", async (_name, isFormaCloud, plan, completedPath, target) => {
    stubServer({ [`/api/app/organizations/${ORG}/billing/summary`]: summary(isFormaCloud, plan) });
    expect(await visit(completedPath)).toBe(target);
  });
});

describe("organization settings for the billing role", () => {
  const loader = OrganizationSettingsRoute.options.loader as unknown as (ctx: {
    context: { queryClient: QueryClient };
    params: { organizationId: string };
    location: { pathname: string };
  }) => Promise<void>;

  const visit = (pathname: string) =>
    redirectOf(() =>
      loader({
        context: { queryClient: new QueryClient() },
        params: { organizationId: ORG },
        location: { pathname },
      })
    );

  test("may open the billing page, and is sent there from every other one", async () => {
    const requested = stubServer({
      [`/api/app/organizations/${ORG}`]: { id: ORG, name: "Acme", membership: { role: "billing" } },
      [`/api/app/organizations/${ORG}/navigation`]: {
        path: `/organizations/${ORG}/settings/billing`,
        canCreateWorkspace: false,
      },
    });
    expect(await visit(`/organizations/${ORG}/settings/billing`)).toBeNull();
    expect(requested).toEqual([`/api/app/organizations/${ORG}`]);
    expect(await visit(`/organizations/${ORG}/settings/general`)).toBe(
      `/organizations/${ORG}/settings/billing`
    );
  });
});
