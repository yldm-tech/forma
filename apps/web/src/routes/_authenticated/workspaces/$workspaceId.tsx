import { useQuery, useQueryClient, useSuspenseQuery } from "@tanstack/react-query";
import { Link, Outlet, createFileRoute, useRouterState } from "@tanstack/react-router";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { AppShell } from "../../../components/shell/app-shell";
import { MainNavigation } from "../../../components/shell/main-navigation";
import { NotFound, RoutePending } from "../../../components/shell/route-states";
import { SettingsNavigation } from "../../../components/shell/settings-navigation";
import { TopControlBar } from "../../../components/shell/top-control-bar";
import { Button } from "../../../components/ui/button";
import { Wordmark } from "../../../components/ui/logo";
import { LimitsReachedBanner } from "../../../features/billing/components/limits-reached-banner";
import { ApiError } from "../../../lib/api";
import { workspaceHomePath } from "../../../lib/navigation";
import { guardPageError } from "../../../lib/route-guards";
import { meQuery } from "../../../lib/queries/auth";
import { billingSummaryQuery } from "../../../lib/queries/billing";
import { organizationQuery } from "../../../lib/queries/organizations";
import { workspaceQuery } from "../../../lib/queries/workspaces";
import { rememberWorkspace } from "../../../lib/storage";
import { PostHogGroupIdentify } from "../../../lib/telemetry/posthog-identify";

export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId")({
  loader: async ({ context: { queryClient }, params }) => {
    const workspace = await queryClient
      .ensureQueryData(workspaceQuery(params.workspaceId))
      .catch(guardPageError);
    await queryClient.ensureQueryData(organizationQuery(workspace.organizationId)).catch(guardPageError);
  },
  pendingComponent: RoutePending,
  component: WorkspaceLayout,
  notFoundComponent: WorkspaceNotFound,
});

/** Whether the workspace's own request was refused (403) or found nothing (404); guardPageError turns both into a not-found, which this layout tells apart from a missing page inside a workspace the caller can open. */
const isWorkspaceUnavailable = (error: unknown): boolean =>
  error instanceof ApiError && (error.status === 403 || error.status === 404);

/**
 * What the workspace layout shows for a not-found. A workspace the caller cannot open gets its own explanation, since it may well exist: a member outside its teams is told to ask for access, not that the page is missing. Any other not-found (an unknown path, a survey that is gone) is the 404 page.
 *
 * A not-found thrown by a loader replaces the whole layout, so it is drawn as a page of its own: the wordmark over the message, centred, as the root not-found page lays it out, with the shared NotFound for the 404. An unknown path is drawn inside the shell, in place of the page; the shell already has the page's <main>, so that 404 has its own markup, linking back to the workspace instead of the root.
 */
function WorkspaceNotFound() {
  const { t } = useTranslation();
  const { workspaceId } = Route.useParams();
  const queryClient = useQueryClient();
  const isStandalone = Route.useMatch({ select: (match) => match.status === "notFound" });
  const noAccess = isWorkspaceUnavailable(
    queryClient.getQueryState(workspaceQuery(workspaceId).queryKey)?.error
  );

  if (!isStandalone) {
    return (
      <div className="mx-auto flex h-full max-w-xl flex-col items-center justify-center p-8 text-center">
        <p className="text-sm font-semibold text-slate-900">404</p>
        <h1 className="mt-2 text-2xl font-bold text-slate-900">{t("common.page_not_found")}</h1>
        <p className="mt-2 text-base text-slate-600">{t("common.page_not_found_description")}</p>
        <Button asChild className="mt-8">
          <Link to={workspaceHomePath(workspaceId)}>{t("common.back_to_home")}</Link>
        </Button>
      </div>
    );
  }
  return (
    <div className="flex min-h-dvh flex-col items-center justify-center bg-slate-50 px-4">
      <Wordmark className="text-2xl" />
      {noAccess ? (
        <main className="mx-auto flex max-w-xl flex-col items-center justify-center py-16 text-center">
          <h1 className="text-2xl font-bold text-slate-900">{t("workspace.no_access_title")}</h1>
          <p className="mt-2 text-base text-slate-600">{t("workspace.no_access_description")}</p>
          <Button asChild className="mt-8">
            <Link to="/">{t("workspace.no_access_cta")}</Link>
          </Button>
        </main>
      ) : (
        <NotFound />
      )}
    </div>
  );
}

function WorkspaceLayout() {
  const { workspaceId } = Route.useParams();
  const { data: me } = useSuspenseQuery(meQuery);
  const { data: workspace } = useSuspenseQuery(workspaceQuery(workspaceId));
  const { data: organization } = useSuspenseQuery(organizationQuery(workspace.organizationId));
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const { data: billing } = useQuery(billingSummaryQuery(organization.id));
  const isTrialing = billing?.subscriptionStatus === "trialing";

  // The root page reopens the workspace the browser used last.
  useEffect(() => rememberWorkspace(workspaceId), [workspaceId]);

  // forma2 swaps the whole sidebar for the settings one on any path under the workspace's settings.
  const isSettingsMode = pathname.includes("/settings");

  return (
    <AppShell
      banner={
        // forma2 left the limits banner out during a trial, whose own notice is in the sidebar.
        billing?.isFormaCloud &&
        billing.usage &&
        !isTrialing && (
          <LimitsReachedBanner
            organizationId={organization.id}
            limits={billing.limits}
            responseCount={billing.usage.responses}
          />
        )
      }
      sidebar={
        isSettingsMode ? (
          <SettingsNavigation
            organizationId={organization.id}
            organizationName={organization.name}
            membershipRole={organization.membership.role}
            workspace={{ id: workspace.id, name: workspace.name }}
            backUrl={workspaceHomePath(workspace.id)}
            pathname={pathname}
            isFormaCloud={billing?.isFormaCloud}
          />
        ) : (
          <MainNavigation
            workspaceId={workspace.id}
            organizationId={organization.id}
            membershipRole={organization.membership.role}
            pathname={pathname}
            trialDaysRemaining={billing?.isFormaCloud ? billing.trialDaysRemaining : null}
          />
        )
      }
      topBar={
        <TopControlBar
          user={me}
          organization={organization}
          workspace={workspace}
          membershipRole={organization.membership.role}
        />
      }>
      <PostHogGroupIdentify
        organizationId={organization.id}
        organizationName={organization.name}
        workspaceId={workspace.id}
        workspaceName={workspace.name}
      />
      <Outlet />
    </AppShell>
  );
}
