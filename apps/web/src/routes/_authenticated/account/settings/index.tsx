import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/_authenticated/account/settings/")({
  beforeLoad: () => {
    throw redirect({ to: "/account/settings/profile", replace: true });
  },
});
