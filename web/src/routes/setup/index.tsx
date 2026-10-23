import { createFileRoute, redirect } from "@tanstack/react-router";
import { homeQuery } from "../../lib/queries/organizations";

/** The paths of the root page's answer that /setup forwards to; any other answer goes to the root page, which decides. */
const SETUP_PATHS = new Set(["/setup/intro", "/setup/organization/create"]);

// forma2's /setup: a fresh instance goes to its intro, a signed-in user without an organization to create one, and everyone else to the root page. The root page's answer (GET /api/app/home) already makes the first two decisions, so /setup follows it. A signed-out visitor of an instance that has users is sent to sign in by the root page, which then sends a user without an organization on to create one, as forma2's callback to /setup/organization/create did.
export const Route = createFileRoute("/setup/")({
  beforeLoad: async ({ context }) => {
    const home = await context.queryClient.fetchQuery(homeQuery);
    throw redirect({ href: home.path && SETUP_PATHS.has(home.path) ? home.path : "/", replace: true });
  },
});
