import { createFileRoute, redirect } from "@tanstack/react-router";
import { homeTarget } from "../lib/home";
import { homeQuery } from "../lib/queries/organizations";
import { WORKSPACE_ID_KEY, readStorage } from "../lib/storage";

// forma2's root page: the server decides between the setup intro, sign-in, creating an organization, onboarding and the landing page; otherwise the browser reopens the workspace it last used, or the first one the user can open.
export const Route = createFileRoute("/")({
  beforeLoad: async ({ context }) => {
    const home = await context.queryClient.fetchQuery(homeQuery);
    const target = homeTarget(home, readStorage(WORKSPACE_ID_KEY));
    if (target) throw redirect({ href: target, replace: true });
  },
});
