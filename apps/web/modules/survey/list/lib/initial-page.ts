import "server-only";
import { serializeV3SurveyListItem } from "@/app/api/v3/surveys/serializers";
import { getSurveyCount, getWorkspaceSurveyCount } from "@/modules/survey/list/lib/survey";
import { getSurveyListPage } from "@/modules/survey/list/lib/survey-page";
import type { TSurveyListItem } from "@/modules/survey/list/types/survey-overview";
import { initialFilters } from "./constants";

/**
 * Page one of the survey list, with the default filters, read on the server so the list has rows to
 * render instead of a skeleton.
 *
 * The list is a client component whose query is gated on a `isFilterInitialized` flag set from a
 * localStorage effect, so the first request for a workspace's surveys could not leave the browser
 * until after hydration. The server was already rendering the page; it just was not sending the one
 * thing the page is for.
 *
 * Returns the shape the list already renders. The `/api/v3` client exists to turn JSON back into
 * `Date`s; RSC carries `Date`s across on its own, so making the server stringify them just so the
 * client could parse them again would be a round trip that buys nothing.
 *
 * Only the default filters. A visitor whose stored filters differ gets a cache miss and the ordinary
 * fetch, which is exactly what happens today; seeding their query key with someone else's filters
 * would show them the wrong rows.
 */
export const getInitialSurveyListPage = async (
  workspaceId: string,
  limit: number
): Promise<{
  data: TSurveyListItem[];
  meta: { limit: number; nextCursor: string | null; totalCount: number; workspaceSurveyCount: number };
}> => {
  const [surveyPage, totalCount, workspaceSurveyCount] = await Promise.all([
    getSurveyListPage(workspaceId, {
      limit,
      cursor: null,
      sortBy: initialFilters.sortBy,
      filterCriteria: {},
    }),
    getSurveyCount(workspaceId, {}),
    getWorkspaceSurveyCount(workspaceId),
  ]);

  return {
    data: surveyPage.surveys.map((survey) => ({
      ...serializeV3SurveyListItem(survey),
      // Absent from the v3 response too — see the note on TV3SurveyListItemResponse. Kept identical
      // here so a seeded page and a fetched one cannot differ.
      singleUse: null,
    })),
    meta: {
      limit,
      nextCursor: surveyPage.nextCursor,
      totalCount,
      workspaceSurveyCount,
    },
  };
};
