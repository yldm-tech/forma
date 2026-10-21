import { createFileRoute, redirect } from "@tanstack/react-router";
import { guardPageError } from "../../../../lib/route-guards";
import { organizationNavigationQuery } from "../../../../lib/queries/organizations";

// forma2's /organizations/{id}: the first workspace the user can open, the landing page when there is none, or the billing path for a billing member.
export const Route = createFileRoute("/_authenticated/organizations/$organizationId/")({
  beforeLoad: async ({ context: { queryClient }, params }) => {
    const navigation = await queryClient
      .fetchQuery(organizationNavigationQuery(params.organizationId))
      .catch(guardPageError);
    throw redirect({ href: navigation.path, replace: true });
  },
});
