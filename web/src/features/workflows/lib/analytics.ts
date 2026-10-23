import {
  type PostHogClientEventProperties,
  capturePostHogClientEvent,
  capturePostHogClientEventWhenReady,
} from "../../../lib/telemetry/posthog";
import type { TWorkflowValidationProblem } from "../state/editor";

// forma2's Workflows UI events (modules/workflows/lib/analytics-events.ts and analytics.ts). The lifecycle events (workflow_created, ...) and the run and usage events are the server's.

/** Client-side events, fired from the Workflows UI through trackWorkflowEvent. */
export const WORKFLOW_CLIENT_EVENTS = {
  surfaceViewed: "workflow_surface_viewed",
  triggerAdded: "workflow_trigger_added",
  actionAdded: "workflow_action_added",
  nodeDeleted: "workflow_node_deleted",
  inspectorOpened: "workflow_inspector_opened",
  validationProblemsViewed: "workflow_validation_problems_viewed",
  validationProblemFixClicked: "workflow_validation_problem_fix_clicked",
  enableBlocked: "workflow_enable_blocked",
  autosaveFailed: "workflow_autosave_failed",
  listFiltered: "workflow_list_filtered",
  runDetailOpened: "workflow_run_detail_opened",
} as const;
export type TWorkflowClientEvent = (typeof WORKFLOW_CLIENT_EVENTS)[keyof typeof WORKFLOW_CLIENT_EVENTS];

/** The distinct screens of the feature, reported as surface on workflow_surface_viewed. The inspector and the run drawer have richer events of their own. */
export const WORKFLOW_SURFACES = [
  "list",
  "list_empty",
  "list_empty_filtered",
  "workspace_runs",
  "builder",
  "workflow_runs",
] as const;
export type TWorkflowSurface = (typeof WORKFLOW_SURFACES)[number];

/** Captures a Workflows event behind a user action; a no-op where PostHog is not configured. The organization and workspace groups are already on the session (PostHogGroupIdentify), so callers pass event facts only. */
export const trackWorkflowEvent = (
  event: TWorkflowClientEvent,
  properties?: PostHogClientEventProperties
): void => capturePostHogClientEvent(event, properties);

/** The same for events that fire on mount (a screen being viewed): it waits for PostHog to finish starting and returns a canceller for the effect cleanup. */
export const trackWorkflowEventWhenReady = (
  event: TWorkflowClientEvent,
  properties?: PostHogClientEventProperties
): (() => void) => capturePostHogClientEventWhenReady(event, properties);

/** The problem list as analytics properties: a count and the distinct codes, sorted so the same set always lands in the same breakdown bucket. Codes only, never the field paths, which can carry node ids. */
export const summarizeValidationProblems = (
  problems: readonly TWorkflowValidationProblem[]
): { problem_count: number; problem_codes: string[] } => ({
  problem_count: problems.length,
  problem_codes: [...new Set(problems.map((problem) => problem.code))].sort((a, b) => a.localeCompare(b)),
});

export interface WorkflowListSurfaceState {
  showInitialLoading: boolean;
  isError: boolean;
  /** A status filter or a search term is set, so an empty list is an emptied filter. */
  hasActiveFilters: boolean;
  workflowCount: number;
}

/** Which of the three list states the user is looking at, the way the page picks its body: null while the skeleton or the error state stands in for the list, neither of which is a visit. An error with rows still on screen is a visit to the list. */
export const resolveWorkflowListSurface = (state: WorkflowListSurfaceState): TWorkflowSurface | null => {
  if (state.showInitialLoading) return null;
  if (state.workflowCount > 0) return "list";
  if (state.isError) return null;
  return state.hasActiveFilters ? "list_empty_filtered" : "list_empty";
};
