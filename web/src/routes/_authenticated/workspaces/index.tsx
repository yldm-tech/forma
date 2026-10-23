import { createFileRoute, redirect } from "@tanstack/react-router";

// forma2's /workspaces has no page of its own: the root page decides which workspace to open.
export const Route = createFileRoute("/_authenticated/workspaces/")({
  beforeLoad: () => {
    throw redirect({ to: "/", replace: true });
  },
});
