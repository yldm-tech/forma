import { useSuspenseQuery } from "@tanstack/react-query";
import { Outlet, createFileRoute, notFound } from "@tanstack/react-router";
import { RouteError, RoutePending } from "../../../../../../components/shell/route-states";
import { analysisSurveyQuery } from "../../../../../../features/survey-analysis/api";
import { ResponseFilterProvider } from "../../../../../../features/survey-analysis/components/filter/response-filter-context";
import type { AnalysisSearch } from "../../../../../../features/survey-analysis/components/survey-analysis-cta";
import { guardPageError } from "../../../../../../lib/route-guards";

// A survey's analysis views, forma2's (analysis) route group: the summary and the responses share the filter, which lives here so it survives switching between them.
export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/surveys/$surveyId")({
  validateSearch: (search: Record<string, unknown>): AnalysisSearch => ({
    // The router parses the param's JSON into a value; the provider validates its shape.
    ...(search.filter === undefined ? {} : { filter: search.filter }),
    ...(search.share === true ? { share: true } : {}),
    ...(search.success === true ? { success: true } : {}),
    ...(search.scheduled === true ? { scheduled: true } : {}),
  }),
  loader: async ({ context: { queryClient }, params }) => {
    const survey = await queryClient
      .ensureQueryData(analysisSurveyQuery(params.surveyId))
      .catch(guardPageError);
    // forma2 answered a survey opened under another workspace's URL with a not-found.
    if (survey.workspaceId !== params.workspaceId) throw notFound();
  },
  pendingComponent: RoutePending,
  errorComponent: RouteError,
  component: SurveyAnalysisRoute,
});

function SurveyAnalysisRoute() {
  const { surveyId } = Route.useParams();
  const { data: survey } = useSuspenseQuery(analysisSurveyQuery(surveyId));
  return (
    <ResponseFilterProvider key={survey.id} survey={survey}>
      <Outlet />
    </ResponseFilterProvider>
  );
}
