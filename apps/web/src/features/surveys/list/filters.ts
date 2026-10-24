// The survey list's filters, forma2's modules/survey/list/lib/{constants,utils}.ts: what the toolbar can ask for, how a stored or typed value is normalized before it reaches the query, and where the browser remembers them.

/** Where this browser keeps a workspace's survey filters. Each workspace has its own, so a filter set in one does not empty another's list. forma2 kept one key ("forma-surveys-filters") for every workspace; that value is no longer read. */
export const surveyFiltersKey = (workspaceId: string): string => `forma-surveys-filters:${workspaceId}`;

/** forma2's SURVEYS_PER_PAGE. */
export const SURVEYS_PER_PAGE = 12;

export type SurveyStatus = "draft" | "inProgress" | "paused" | "completed";
/** "archived" is a pseudo-status of the filter only; the server turns it into an archivedAt condition. */
export type SurveyStatusFilter = SurveyStatus | "archived";
export type SurveyTypeFilter = "link" | "app";
export type SurveySort = "createdAt" | "updatedAt" | "name" | "relevance";
/** A workspace's configured channel, forma2's TWorkspaceConfigChannel. */
export type WorkspaceChannel = "link" | "app" | "website" | null;

export interface SurveyFilters {
  name: string;
  status: SurveyStatusFilter[];
  type: SurveyTypeFilter[];
  sortBy: SurveySort;
}

export const initialFilters: SurveyFilters = { name: "", status: [], type: [], sortBy: "relevance" };

const allowedStatus = new Set<unknown>(["draft", "inProgress", "paused", "completed", "archived"]);
const allowedType = new Set<unknown>(["app", "link"]);
const allowedSort = new Set<unknown>(["createdAt", "updatedAt", "name", "relevance"]);

const normalizedList = <T extends string>(value: unknown, allowed: Set<unknown>): T[] =>
  Array.isArray(value)
    ? [...new Set(value.filter((v): v is T => allowed.has(v)))].sort((a, b) => a.localeCompare(b))
    : [];

/** forma2's normalizeSurveyFilters: unknown values dropped, lists de-duplicated and sorted, the name trimmed, and no type filter in a link-only workspace, whose toolbar has no type control. */
export const normalizeSurveyFilters = (
  filters: Partial<SurveyFilters> | null | undefined,
  channel?: WorkspaceChannel
): SurveyFilters => ({
  name: typeof filters?.name === "string" ? filters.name.trim() : initialFilters.name,
  status: normalizedList<SurveyStatusFilter>(filters?.status, allowedStatus),
  type: channel === "link" ? [] : normalizedList<SurveyTypeFilter>(filters?.type, allowedType),
  sortBy: allowedSort.has(filters?.sortBy) ? (filters?.sortBy as SurveySort) : initialFilters.sortBy,
});

/** forma2's parseStoredSurveyFilters: null when nothing is stored or the stored value is not JSON. */
export const parseStoredSurveyFilters = (
  stored: string | null,
  channel?: WorkspaceChannel
): SurveyFilters | null => {
  if (!stored) return null;
  try {
    return normalizeSurveyFilters(JSON.parse(stored) as Partial<SurveyFilters>, channel);
  } catch {
    return null;
  }
};

/** What the browser keeps of the filters: status, type and sort order. The name search is left out, so coming back later does not show a list narrowed by a search typed in an earlier visit. */
export const storableSurveyFilters = (filters: SurveyFilters): Omit<SurveyFilters, "name"> => ({
  status: filters.status,
  type: filters.type,
  sortBy: filters.sortBy,
});

/** The filters a list opens with: what the browser kept for the workspace, with an empty name search. */
export const restoredSurveyFilters = (stored: string | null, channel?: WorkspaceChannel): SurveyFilters => {
  const parsed = parseStoredSurveyFilters(stored, channel);
  return parsed ? { ...parsed, name: initialFilters.name } : initialFilters;
};

/** Whether the toolbar offers "Clear filters": a status, a type or a name is set; the sort order alone does not count. */
export const hasActiveFilters = (filters: SurveyFilters): boolean =>
  filters.status.length > 0 || filters.type.length > 0 || filters.name !== "";
