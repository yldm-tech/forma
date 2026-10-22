// The survey list's filters, forma2's modules/survey/list/lib/{constants,utils}.ts: what the toolbar can ask for, how a stored or typed value is normalized before it reaches the query, and where the browser remembers them.

/** forma2's FORMA_SURVEYS_FILTERS_KEY_LS. */
export const SURVEY_FILTERS_KEY = "forma-surveys-filters";

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

/** Whether the toolbar offers "Clear filters": a status, a type or a name is set; the sort order alone does not count. */
export const hasActiveFilters = (filters: SurveyFilters): boolean =>
  filters.status.length > 0 || filters.type.length > 0 || filters.name !== "";
