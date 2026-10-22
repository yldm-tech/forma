import { QueryClient } from "@tanstack/react-query";
import { createRouter } from "@tanstack/react-router";
import { expect, test } from "vite-plus/test";
import { routeTree } from "./routeTree.gen";

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
  "/workspaces/0190a000-0000-7000-8000-000000000002/",
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
