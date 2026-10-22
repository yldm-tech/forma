import { z } from "zod";
import { type TWorkflowStatus, ZWorkflowSortBy, ZWorkflowStatus } from "../definition";

// The workflows list toolbar's filters, forma2's modules/workflows/lib/list-filters.ts.

/** forma2's FORMA_WORKFLOWS_FILTERS_KEY_LS. */
export const WORKFLOW_FILTERS_KEY = "forma-workflows-filters";

/** forma2's WORKFLOWS_PER_PAGE. */
export const WORKFLOWS_PER_PAGE = 12;

/** The status selection as the list API's filter[status][in]: an empty selection is undefined, which the API reads as every status except archived. "archived" is one more option, listed alongside any other selected status when picked. */
export const computeStatusIn = (selectedStatuses: TWorkflowStatus[]): TWorkflowStatus[] | undefined =>
  selectedStatuses.length === 0 ? undefined : selectedStatuses;

const ZStoredWorkflowFilters = z.object({
  searchValue: z.string(),
  selectedStatuses: z.array(ZWorkflowStatus),
  sortBy: ZWorkflowSortBy,
});

export type TStoredWorkflowFilters = z.infer<typeof ZStoredWorkflowFilters>;

export const initialWorkflowFilters: TStoredWorkflowFilters = {
  searchValue: "",
  selectedStatuses: [],
  sortBy: "updatedAt",
};

/** The filters stored in localStorage, or null when nothing is stored or the value is malformed or stale, so the toolbar falls back to its defaults. */
export const parseStoredWorkflowFilters = (raw: string | null): TStoredWorkflowFilters | null => {
  if (!raw) return null;
  try {
    const result = ZStoredWorkflowFilters.safeParse(JSON.parse(raw));
    return result.success ? result.data : null;
  } catch {
    return null;
  }
};
