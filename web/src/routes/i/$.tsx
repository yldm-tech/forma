import { createFileRoute, redirect } from "@tanstack/react-router";

// forma2 sent every /i/{path} to /{path} (next.config.mjs), the query kept.
export const Route = createFileRoute("/i/$")({
  beforeLoad: ({ params, location }) => {
    throw redirect({ href: `/${params._splat ?? ""}${location.searchStr}`, replace: true });
  },
});
