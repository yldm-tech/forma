import { QueryClient } from "@tanstack/react-query";
import { createRouter } from "@tanstack/react-router";
import { expect, test } from "vite-plus/test";
import { routeTree } from "./routeTree.gen";
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
  "/workspaces/0190a000-0000-7000-8000-000000000002/",
  // The integrations' OAuth callbacks (server/internal/integration/connect.go callbackPaths).
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
