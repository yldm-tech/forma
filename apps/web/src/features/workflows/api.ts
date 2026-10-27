import type { InfiniteData } from "@tanstack/react-query";
import { ApiError, api } from "../../lib/api";
import type { TFollowUpEmailToUser } from "../survey-editor/types/survey-follow-up";
import type {
  TWorkflowDefinition,
  TWorkflowRunLogStatus,
  TWorkflowRunStatus,
  TWorkflowSortBy,
  TWorkflowStatus,
  TWorkflowTriggerType,
} from "./definition";

// The /api/v3/workflows client (apps/server/internal/workflow/handler.go), forma2's modules/workflows/lib/api-client.ts and lib/query.ts. Timestamps are ISO strings.

/** forma2's ZWorkflowRunSummary, the run shape a workflow embeds as its lastRun. */
export interface TWorkflowRunSummary {
  id: string;
  workflowId: string;
  workspaceId: string;
  workflowVersionId: string | null;
  status: TWorkflowRunStatus;
  isDryRun: boolean;
  triggerType: TWorkflowTriggerType;
  surveyId: string | null;
  responseId: string | null;
  error: string | null;
  attempt: number;
  createdAt: string;
  updatedAt: string;
  startedAt: string | null;
  finishedAt: string | null;
  /** How many failed steps a completed run went on past, its steps being set to continue on failure: such a run completed, but not cleanly. 0 for any other run. */
  continuedFailureCount: number;
}

/** A row of the runs list: the summary and its workflow's name. */
export interface TWorkflowRunListItem extends TWorkflowRunSummary {
  workflowName: string;
}

/** One step of a run, every field explicit. */
export interface TWorkflowRunLog {
  id: string;
  runId: string;
  sequence: number;
  stepId: string;
  stepType: string;
  status: TWorkflowRunLogStatus;
  input: Record<string, unknown>;
  output: Record<string, unknown>;
  error: string | null;
  startedAt: string | null;
  finishedAt: string | null;
}

/** A run's detail: its trigger payload, its data and its steps in sequence order. */
export interface TWorkflowRunDetail extends TWorkflowRunSummary {
  triggerPayload: Record<string, unknown>;
  data: Record<string, unknown>;
  logs: TWorkflowRunLog[];
  idempotencyKey: string | null;
  nextAttemptAt: string | null;
  lastErrorAt: string | null;
}

/** forma2's ZWorkflowListItem: a workflow without its definition. */
export interface TWorkflowListItem {
  id: string;
  workspaceId: string;
  name: string;
  description: string | null;
  status: TWorkflowStatus;
  triggerType: TWorkflowTriggerType | null;
  surveyId: string | null;
  createdBy: string | null;
  creator: { name: string } | null;
  createdAt: string;
  updatedAt: string;
  lastRun: TWorkflowRunSummary | null;
  runCount: number;
}

/** forma2's ZWorkflowResource: the list item and the definition. */
export interface TWorkflowResource extends TWorkflowListItem {
  definition: TWorkflowDefinition;
}

export interface TCreateWorkflowInput {
  workspaceId: string;
  name: string;
  description?: string | null;
  status?: "draft";
  definition: TWorkflowDefinition;
}

export interface TPatchWorkflowInput {
  name?: string;
  description?: string | null;
  definition?: TWorkflowDefinition;
}

interface ListMeta {
  limit: number;
  nextCursor: string | null;
}

export interface TWorkflowListPage {
  data: TWorkflowListItem[];
  meta: ListMeta;
}

export interface TWorkflowRunListPage {
  data: TWorkflowRunListItem[];
  meta: ListMeta;
}

export interface TWorkflowListFilters {
  nameContains?: string;
  statusIn?: TWorkflowStatus[];
  sortBy?: TWorkflowSortBy;
}

export interface TWorkflowRunListFilters {
  workflowId?: string;
  responseId?: string;
  statusIn?: TWorkflowRunStatus[];
  isDryRun?: boolean;
}

/** What the send_email inspector authors against (GET /api/app/workspaces/{id}/workflows/authoring-context): who can be addressed and the address mail leaves from. */
export interface TWorkflowAuthoringContextWire {
  teamMemberDetails: TFollowUpEmailToUser[];
  mailFrom: string;
}

/** A test send's outcome (POST /api/app/workflows/{id}/steps/{stepId}/test-send): whether the receiver accepted it, its status, and why it failed, as a reason code the builder translates and as the server's English detail. */
export interface TWorkflowTestSendResult {
  ok: boolean;
  status?: number;
  reason?:
    | "url_refused"
    | "timeout"
    | "unreachable"
    | "redirect"
    | "status"
    | "secret_unreadable"
    | "invalid_config";
  message?: string;
}

// Bounds mutation calls so a stalled request cannot leave the editor's save or transition spinner stuck; the rejection surfaces as the normal error toast.
export const MUTATION_TIMEOUT_MS = 15_000;

const mutationSignal = () => ({ signal: AbortSignal.timeout(MUTATION_TIMEOUT_MS) });

export const buildWorkflowListSearchParams = ({
  workspaceId,
  limit,
  cursor,
  filters,
}: {
  workspaceId: string;
  limit: number;
  cursor?: string | null;
  filters?: TWorkflowListFilters;
}): URLSearchParams => {
  const searchParams = new URLSearchParams();
  searchParams.set("workspaceId", workspaceId);
  searchParams.set("limit", String(limit));
  if (filters?.sortBy) searchParams.set("sortBy", filters.sortBy);
  if (cursor) searchParams.set("cursor", cursor);
  const trimmedName = filters?.nameContains?.trim();
  if (trimmedName) searchParams.set("filter[name][contains]", trimmedName);
  for (const status of filters?.statusIn ?? []) searchParams.append("filter[status][in]", status);
  return searchParams;
};

export const buildWorkflowRunListSearchParams = ({
  workspaceId,
  limit,
  cursor,
  filters,
}: {
  workspaceId: string;
  limit: number;
  cursor?: string | null;
  filters?: TWorkflowRunListFilters;
}): URLSearchParams => {
  const searchParams = new URLSearchParams();
  searchParams.set("workspaceId", workspaceId);
  searchParams.set("limit", String(limit));
  if (cursor) searchParams.set("cursor", cursor);
  if (filters?.workflowId) searchParams.set("workflowId", filters.workflowId);
  if (filters?.responseId) searchParams.set("responseId", filters.responseId);
  if (filters?.isDryRun !== undefined) searchParams.set("filter[isDryRun][eq]", String(filters.isDryRun));
  for (const status of filters?.statusIn ?? []) searchParams.append("filter[status][in]", status);
  return searchParams;
};

const unwrap = async (request: Promise<{ data: TWorkflowResource }>) => (await request).data;

const lifecycle = (workflowId: string, action: "enable" | "disable" | "archive" | "unarchive") =>
  unwrap(
    api.post<{ data: TWorkflowResource }>(
      `/api/v3/workflows/${workflowId}/${action}`,
      undefined,
      mutationSignal()
    )
  );

export const workflowsApi = {
  list: (input: Parameters<typeof buildWorkflowListSearchParams>[0], signal?: AbortSignal) =>
    api.get<TWorkflowListPage>(`/api/v3/workflows?${buildWorkflowListSearchParams(input).toString()}`, {
      signal,
    }),
  get: (workflowId: string, signal?: AbortSignal) =>
    unwrap(api.get<{ data: TWorkflowResource }>(`/api/v3/workflows/${workflowId}`, { signal })),
  create: (input: TCreateWorkflowInput) =>
    unwrap(api.post<{ data: TWorkflowResource }>("/api/v3/workflows", input, mutationSignal())),
  update: (workflowId: string, patch: TPatchWorkflowInput) =>
    unwrap(
      api.patch<{ data: TWorkflowResource }>(`/api/v3/workflows/${workflowId}`, patch, mutationSignal())
    ),
  duplicate: (workflowId: string, name?: string) =>
    unwrap(
      api.post<{ data: TWorkflowResource }>(
        `/api/v3/workflows/${workflowId}/duplicate`,
        name ? { name } : {},
        mutationSignal()
      )
    ),
  remove: (workflowId: string) => api.delete<void>(`/api/v3/workflows/${workflowId}`, mutationSignal()),
  enable: (workflowId: string) => lifecycle(workflowId, "enable"),
  disable: (workflowId: string) => lifecycle(workflowId, "disable"),
  archive: (workflowId: string) => lifecycle(workflowId, "archive"),
  unarchive: (workflowId: string) => lifecycle(workflowId, "unarchive"),
  listRuns: (input: Parameters<typeof buildWorkflowRunListSearchParams>[0], signal?: AbortSignal) =>
    api.get<TWorkflowRunListPage>(
      `/api/v3/workflows/runs?${buildWorkflowRunListSearchParams(input).toString()}`,
      { signal }
    ),
  getRun: async (runId: string, signal?: AbortSignal) =>
    (await api.get<{ data: TWorkflowRunDetail }>(`/api/v3/workflows/runs/${runId}`, { signal })).data,
  testSend: (workflowId: string, stepId: string) =>
    api.post<TWorkflowTestSendResult>(
      `/api/app/workflows/${workflowId}/steps/${encodeURIComponent(stepId)}/test-send`,
      undefined,
      mutationSignal()
    ),
  authoringContext: (workspaceId: string, signal?: AbortSignal) =>
    api.get<TWorkflowAuthoringContextWire>(`/api/app/workspaces/${workspaceId}/workflows/authoring-context`, {
      signal,
    }),
};

// ---- Query keys and cache helpers ----

export interface TWorkflowListKeyInput {
  workspaceId: string;
  limit: number;
  nameContains: string;
  statusIn?: TWorkflowStatus[];
  sortBy?: TWorkflowSortBy;
}

export const workflowKeys = {
  all: ["workflows"] as const,
  lists: () => [...workflowKeys.all, "list"] as const,
  list: (input: TWorkflowListKeyInput) => [...workflowKeys.lists(), input] as const,
  details: () => [...workflowKeys.all, "detail"] as const,
  detail: (workflowId: string) => [...workflowKeys.details(), workflowId] as const,
  authoringContext: (workspaceId: string) => [...workflowKeys.all, "authoring-context", workspaceId] as const,
};

export type TWorkflowListKey = ReturnType<typeof workflowKeys.list>;

export interface TWorkflowRunListKeyInput {
  workspaceId: string;
  limit: number;
  filters?: TWorkflowRunListFilters;
}

export const workflowRunKeys = {
  all: ["workflow-runs"] as const,
  lists: () => [...workflowRunKeys.all, "list"] as const,
  list: (input: TWorkflowRunListKeyInput) => [...workflowRunKeys.lists(), input] as const,
  details: () => [...workflowRunKeys.all, "detail"] as const,
  detail: (runId: string) => [...workflowRunKeys.details(), runId] as const,
};

export const flattenWorkflowPages = (data?: InfiniteData<TWorkflowListPage>): TWorkflowListItem[] =>
  data?.pages.flatMap((page) => page.data) ?? [];

export const flattenWorkflowRunPages = (data?: InfiniteData<TWorkflowRunListPage>): TWorkflowRunListItem[] =>
  data?.pages.flatMap((page) => page.data) ?? [];

/** Drops a workflow from the cached pages; the data is returned unchanged when the workflow is not in it. */
export const removeWorkflowFromInfiniteData = (
  data: InfiniteData<TWorkflowListPage> | undefined,
  workflowId: string
): InfiniteData<TWorkflowListPage> | undefined => {
  if (!data || !data.pages.some((page) => page.data.some((workflow) => workflow.id === workflowId))) {
    return data;
  }
  return {
    ...data,
    pages: data.pages.map((page) => ({
      ...page,
      data: page.data.filter((workflow) => workflow.id !== workflowId),
    })),
  };
};

// ---- Error messages ----

/** A response in which the API deliberately refused the request. A 5xx says nothing about the request, and anything that is not an ApiError never reached the API (offline, the mutation timeout). */
const isRefusal = (error: unknown): error is ApiError => error instanceof ApiError && error.status < 500;

/** forma2's getWorkflowApiErrorMessage: the API's own detail for a refusal, the caller's copy for anything else, so a browser's "Failed to fetch" never reaches the user. */
export const getWorkflowApiErrorMessage = (error: unknown, fallbackMessage: string): string =>
  isRefusal(error) && error.message.trim() ? error.message : fallbackMessage;

/** Whether a failed save is worth retrying by itself once the connection is back: only a deliberate refusal is not. */
export const classifyWorkflowSaveError = (error: unknown): "unreachable" | "rejected" =>
  isRefusal(error) ? "rejected" : "unreachable";
