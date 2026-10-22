import { createFileRoute, redirect } from "@tanstack/react-router";
import { untilSettled } from "../lib/consistency";
import { homeTarget } from "../lib/home";
import { homeQuery } from "../lib/queries/organizations";
import { WORKSPACE_ID_KEY, readStorage } from "../lib/storage";

// forma2's root page: the server decides between the setup intro, sign-in, creating an organization, onboarding and the landing page; otherwise the browser reopens the workspace it last used, or the first one the user can open. Right after a sign-up, an accepted invite or an SSO sign-in the new membership may not be authorized yet; the answer is then pending and is asked for again rather than sending the user to create an organization or into an older one.
export const Route = createFileRoute("/")({
  beforeLoad: async ({ context }) => {
    const home = await untilSettled(
      () => context.queryClient.fetchQuery(homeQuery),
      (h) => h.pending === true
    );
    const target = homeTarget(home, readStorage(WORKSPACE_ID_KEY));
    if (target) throw redirect({ href: target, replace: true });
  },
});
