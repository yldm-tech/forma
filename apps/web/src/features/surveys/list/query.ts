import type { InfiniteData } from "@tanstack/react-query";
import type { SurveyListItem, SurveyListPage } from "../api";
import type { SurveyFilters } from "./filters";

// The survey list's cache, forma2's modules/survey/list/lib/query.ts: one infinite query per workspace, page size and filter set, and the patches the row actions apply to it before the server answers.

export const surveyKeys = {
  all: ["surveys"] as const,
  lists: () => [...surveyKeys.all, "list"] as const,
  list: (input: { workspaceId: string; limit: number; filters: SurveyFilters }) =>
    [...surveyKeys.lists(), input] as const,
};

export type SurveyListKey = ReturnType<typeof surveyKeys.list>;

export const surveyMutationKeys = {
  /** Shared by archive, restore and delete, so the list can see a removal that is still in flight. */
  removal: () => [...surveyKeys.all, "removal"] as const,
};

export const flattenSurveyPages = (data?: InfiniteData<SurveyListPage>): SurveyListItem[] =>
  data?.pages.flatMap((page) => page.data) ?? [];

/** Applies patch to the survey wherever it is cached; the data is returned unchanged when the survey is not in it. */
export const updateSurveyInInfiniteData = (
  data: InfiniteData<SurveyListPage> | undefined,
  surveyId: string,
  patch: Partial<SurveyListItem>
): InfiniteData<SurveyListPage> | undefined => {
  if (!data || !data.pages.some((page) => page.data.some((survey) => survey.id === surveyId))) return data;
  return {
    ...data,
    pages: data.pages.map((page) => ({
      ...page,
      data: page.data.map((survey) => (survey.id === surveyId ? { ...survey, ...patch } : survey)),
    })),
  };
};

/**
 * Drops a survey from the cached pages and lowers the counts. removesFromWorkspace separates a delete, which takes the survey out of the workspace, from an archive or restore, which only takes it out of this view.
 */
export const removeSurveyFromInfiniteData = (
  data: InfiniteData<SurveyListPage> | undefined,
  surveyId: string,
  { removesFromWorkspace = false }: { removesFromWorkspace?: boolean } = {}
): InfiniteData<SurveyListPage> | undefined => {
  if (!data || !data.pages.some((page) => page.data.some((survey) => survey.id === surveyId))) return data;
  const decrement = (count: number | null) => (count === null ? null : Math.max(0, count - 1));
  return {
    ...data,
    pages: data.pages.map((page) => ({
      ...page,
      data: page.data.filter((survey) => survey.id !== surveyId),
      meta: {
        ...page.meta,
        totalCount: decrement(page.meta.totalCount),
        workspaceSurveyCount: removesFromWorkspace
          ? decrement(page.meta.workspaceSurveyCount)
          : page.meta.workspaceSurveyCount,
      },
    })),
  };
};

/** The survey id a pending removal carries in its variables, if any. */
export const removedSurveyId = (variables: unknown): string | undefined => {
  if (typeof variables !== "object" || variables === null) return undefined;
  const { surveyId } = variables as { surveyId?: unknown };
  return typeof surveyId === "string" ? surveyId : undefined;
};
