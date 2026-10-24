import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { Link, Outlet, createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { RoutePending } from "../../../../../../components/shell/route-states";
import { UserDropdown } from "../../../../../../components/shell/user-dropdown";
import { Wordmark } from "../../../../../../components/ui/logo";
import { onboardingQuery } from "../../../../../../features/onboarding/onboarding";
import { skipOnboarding } from "../../../../../../lib/home";
import { meQuery } from "../../../../../../lib/queries/auth";
import { organizationQuery } from "../../../../../../lib/queries/organizations";
import { guardPageError } from "../../../../../../lib/route-guards";

// forma2's workspaces/new layout: the new-workspace flow is for owners and managers (organization.manage), and anyone else gets a not-found. Every step reads the same onboarding state, asked for afresh on each visit.
export const Route = createFileRoute("/_authenticated/organizations/$organizationId/workspaces/new")({
  beforeLoad: async ({ context: { queryClient }, params }) => {
    const onboarding = await queryClient
      .fetchQuery(onboardingQuery(params.organizationId))
      .catch(guardPageError);
    return { onboarding };
  },
  pendingComponent: RoutePending,
  component: OnboardingLayout,
});

/** The steps under a header that says where the user is and lets them out: the organization, "Skip for now" to its workspace, and the user menu with log out. */
function OnboardingLayout() {
  const { t } = useTranslation();
  const { organizationId } = Route.useParams();
  const { data: me } = useSuspenseQuery(meQuery);
  // The name is a courtesy: the steps work without it, so it neither suspends nor fails the page.
  const organization = useQuery(organizationQuery(organizationId));
  return (
    <div className="flex min-h-dvh flex-col bg-slate-50">
      <header className="flex items-center justify-between gap-4 border-b border-slate-200 bg-white px-6 py-3">
        <div className="flex min-w-0 items-center gap-3">
          <Wordmark className="shrink-0 text-xl" />
          {organization.data && (
            <span className="truncate border-l border-slate-200 pl-3 text-sm text-slate-600">
              {organization.data.name}
            </span>
          )}
        </div>
        <div className="flex shrink-0 items-center gap-4">
          {/* The organization's own route opens its first workspace; the root page then stops sending this browser back here. */}
          <Link
            to="/organizations/$organizationId"
            params={{ organizationId }}
            onClick={() => skipOnboarding(organizationId)}
            className="focus-visible:ring-ring rounded-md px-2 py-1 text-sm font-medium text-slate-600 hover:text-slate-900 focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-hidden">
            {t("organizations.workspaces.new.skip_for_now")}
          </Link>
          {/* The flow is for owners and managers only, never the billing role. */}
          <UserDropdown user={me} organizationId={organizationId} isBilling={false} />
        </div>
      </header>
      <Outlet />
    </div>
  );
}
