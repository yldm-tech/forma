import { type QueryClient, infiniteQueryOptions, queryOptions } from "@tanstack/react-query";
import type { TDisplayWithContact } from "@forma/types/displays";
import type { TSurveyQuota } from "@forma/types/quota";
import type {
  TResponseFilterCriteria,
  TResponseHiddenFieldsFilter,
  TResponseWithQuotas,
  TSurveyContactAttributes,
  TSurveyMetaFieldFilter,
} from "@forma/types/responses";
import type { TSurvey, TSurveySummary } from "@forma/types/surveys/types";
import type { TTag } from "@forma/types/tags";
import { api, downloadFile } from "../../lib/api";
import type { Tag } from "../../lib/queries/tags";
import { type Stamped, stamped } from "../../lib/wire";
import { type SurveyWire, parseSurvey } from "../survey-editor/api";

// The analysis views' reads and writes against the Go server: the summary, filter options and displays (server/internal/analysis/handler.go), the responses (server/internal/response/handler.go), tags and the survey's share settings. Timestamps arrive as ISO strings and are revived into Dates here, once, since the views are ported from forma2's components, which read Dates.

/** forma2's RESPONSES_PER_PAGE: the response table loads 25 at a time. */
export const RESPONSES_PER_PAGE = 25;

/** forma2's DISPLAYS_PER_PAGE: the impressions tab loads 15 at a time. */
export const DISPLAYS_PER_PAGE = 15;

/** The filter options of getSurveyFilterDataAction. */
export interface SurveyFilterData {
  environmentTags: TTag[];
  attributes: TSurveyContactAttributes;
  meta: TSurveyMetaFieldFilter;
  hiddenFields: TResponseHiddenFieldsFilter;
  reservedValues: TSurveyMetaFieldFilter;
  variableValues: TSurveyMetaFieldFilter;
  quotas: TSurveyQuota[];
}

/** POST /api/app/surveys/{id}/reset. */
export interface SurveyResetResult {
  success: boolean;
  deletedResponsesCount: number;
  deletedDisplaysCount: number;
}

/** POST /api/app/surveys/{id}/ai/example-responses. */
export interface ExampleResponsesResult {
  createdCount: number;
}

/** A tag added to or removed from a response. */
export interface ResponseTagChange {
  responseId: string;
  tagId: string;
}

/** One link of POST /api/app/surveys/{id}/single-use-links. */
export interface SingleUseLink {
  suId: string;
  suToken?: string;
}

export type ExportFormat = "csv" | "xlsx";

/** The dry run of the attachment export: how many files the archive would hold, and whether that is over its limit. */
export interface AttachmentCounts {
  fileCount: number;
  responseCount: number;
  exceedsMaxFiles: boolean;
  maxFiles: number;
}

/** forma2's attachment export route (server/internal/analysis/attachments_handler.go), outside /api/app: the browser downloads the ZIP by navigating to it, since an archive of several GB cannot be buffered in the tab. Without criteria it exports every response's files; with dryRun it only counts them. */
export const attachmentsUrl = (surveyId: string, criteria?: TResponseFilterCriteria, dryRun = false) => {
  const params = new URLSearchParams(dryRun ? { dryRun: "true" } : undefined);
  if (criteria) params.set("filters", JSON.stringify(criteria));
  return `/api/surveys/${surveyId}/attachments?${params.toString()}`;
};

type ResponseWire = Omit<TResponseWithQuotas, "createdAt" | "updatedAt" | "tags"> & {
  createdAt: string;
  updatedAt: string;
  tags: Stamped<TTag>[];
};

type DisplayWire = Omit<TDisplayWithContact, "createdAt"> & { createdAt: string };

type FilterDataWire = Omit<SurveyFilterData, "environmentTags" | "quotas"> & {
  environmentTags: Stamped<TTag>[];
  quotas: Stamped<TSurveyQuota>[];
};

export const parseResponse = (raw: ResponseWire): TResponseWithQuotas => ({
  ...raw,
  createdAt: new Date(raw.createdAt),
  updatedAt: new Date(raw.updatedAt),
  tags: raw.tags.map((tag) => stamped<TTag>(tag)),
  ttc: raw.ttc ?? {},
  variables: raw.variables ?? {},
  meta: raw.meta ?? {},
  data: raw.data ?? {},
});

/** A workspace tag from the tags list as the response views type it. */
export const toResponseTag = (tag: Tag): TTag => ({
  id: tag.id,
  name: tag.name,
  workspaceId: tag.workspaceId,
  createdAt: new Date(tag.createdAt),
  updatedAt: new Date(tag.updatedAt),
});

const parseDisplay = (raw: DisplayWire): TDisplayWithContact => ({
  ...raw,
  createdAt: new Date(raw.createdAt),
});

/** The summary as the server sends it, its samples' updatedAt revived. */
const parseSummary = (raw: TSurveySummary): TSurveySummary => ({
  ...raw,
  summary: raw.summary.map((entry) => {
    if ("samples" in entry) {
      return {
        ...entry,
        samples: entry.samples.map((sample) => ({ ...sample, updatedAt: new Date(sample.updatedAt) })),
      } as typeof entry;
    }
    if ("files" in entry) {
      return {
        ...entry,
        files: entry.files.map((file) => ({ ...file, updatedAt: new Date(file.updatedAt) })),
      } as typeof entry;
    }
    return entry;
  }),
});

const surveyPath = (surveyId: string) => `/api/app/surveys/${surveyId}`;

export const analysisKeys = {
  all: (surveyId: string) => ["surveys", surveyId, "analysis"] as const,
  survey: (surveyId: string) => [...analysisKeys.all(surveyId), "survey"] as const,
  summary: (surveyId: string, criteria: TResponseFilterCriteria) =>
    [...analysisKeys.all(surveyId), "summary", criteria] as const,
  filterData: (surveyId: string) => [...analysisKeys.all(surveyId), "filter-data"] as const,
  displays: (surveyId: string) => [...analysisKeys.all(surveyId), "displays"] as const,
  responses: (surveyId: string, criteria: TResponseFilterCriteria) =>
    [...analysisKeys.all(surveyId), "responses", criteria] as const,
  responseCount: (surveyId: string, criteria: TResponseFilterCriteria) =>
    [...analysisKeys.all(surveyId), "response-count", criteria] as const,
};

/** The survey the analysis views show. A cache of its own rather than the editor's, which is the editor's save baseline and must only move when the editor saves. */
export const analysisSurveyQuery = (surveyId: string) =>
  queryOptions({
    queryKey: analysisKeys.survey(surveyId),
    queryFn: ({ signal }): Promise<TSurvey> =>
      api.get<SurveyWire>(surveyPath(surveyId), { signal }).then(parseSurvey),
  });

export const surveySummaryQuery = (surveyId: string, criteria: TResponseFilterCriteria) =>
  queryOptions({
    queryKey: analysisKeys.summary(surveyId, criteria),
    queryFn: ({ signal }) =>
      api
        .post<TSurveySummary>(`${surveyPath(surveyId)}/summary`, { filterCriteria: criteria }, { signal })
        .then(parseSummary),
  });

export const surveyFilterDataQuery = (surveyId: string) =>
  queryOptions({
    queryKey: analysisKeys.filterData(surveyId),
    queryFn: ({ signal }) =>
      api
        .get<FilterDataWire>(`${surveyPath(surveyId)}/filter-data`, { signal })
        .then((raw): SurveyFilterData => ({
          ...raw,
          environmentTags: raw.environmentTags.map((tag) => stamped<TTag>(tag)),
          quotas: raw.quotas.map((quota) => stamped<TSurveyQuota>(quota)),
        })),
  });

/** forma2's getDisplaysWithContactAction, a page of DISPLAYS_PER_PAGE at a time; a short page is the last. */
export const surveyDisplaysQuery = (surveyId: string) =>
  infiniteQueryOptions({
    queryKey: analysisKeys.displays(surveyId),
    initialPageParam: 0,
    queryFn: ({ pageParam, signal }) =>
      api
        .get<DisplayWire[]>(
          `${surveyPath(surveyId)}/displays?limit=${DISPLAYS_PER_PAGE}&offset=${pageParam}`,
          { signal }
        )
        .then((page) => page.map(parseDisplay)),
    getNextPageParam: (lastPage, pages) =>
      lastPage.length === DISPLAYS_PER_PAGE ? pages.length * DISPLAYS_PER_PAGE : undefined,
  });

/** forma2's getResponsesAction, RESPONSES_PER_PAGE at a time, newest first; a short page is the last. */
export const surveyResponsesQuery = (surveyId: string, criteria: TResponseFilterCriteria) =>
  infiniteQueryOptions({
    queryKey: analysisKeys.responses(surveyId, criteria),
    initialPageParam: 0,
    queryFn: ({ pageParam, signal }) =>
      api
        .post<ResponseWire[]>(
          `${surveyPath(surveyId)}/responses/search`,
          { limit: RESPONSES_PER_PAGE, offset: pageParam, filterCriteria: criteria },
          { signal }
        )
        .then((page) => page.map(parseResponse)),
    getNextPageParam: (lastPage, pages) =>
      lastPage.length === RESPONSES_PER_PAGE ? pages.length * RESPONSES_PER_PAGE : undefined,
  });

export const surveyResponseCountQuery = (surveyId: string, criteria: TResponseFilterCriteria) =>
  queryOptions({
    queryKey: analysisKeys.responseCount(surveyId, criteria),
    queryFn: ({ signal }) =>
      api
        .post<{ count: number }>(
          `${surveyPath(surveyId)}/responses/count`,
          { filterCriteria: criteria },
          { signal }
        )
        .then((res) => res.count),
  });

/** Drops every cached analysis read of the survey, after a change to its responses. */
export const invalidateSurveyAnalysis = (queryClient: QueryClient, surveyId: string) =>
  queryClient.invalidateQueries({ queryKey: analysisKeys.all(surveyId) });

export const analysisApi = {
  getResponse: (responseId: string) =>
    api.get<ResponseWire>(`/api/app/responses/${responseId}`).then(parseResponse),
  deleteResponse: (responseId: string, decrementQuotas: boolean) =>
    api.delete<ResponseWire>(`/api/app/responses/${responseId}?decrementQuotas=${decrementQuotas}`),
  addTag: (responseId: string, tagId: string) =>
    api.post<ResponseTagChange>(`/api/app/responses/${responseId}/tags`, { tagId }),
  removeTag: (responseId: string, tagId: string) =>
    api.delete<ResponseTagChange>(`/api/app/responses/${responseId}/tags/${tagId}`),
  createTag: (workspaceId: string, name: string) =>
    api
      .post<Stamped<TTag>>(`/api/app/workspaces/${workspaceId}/tags`, { name })
      .then((tag) => stamped<TTag>(tag)),
  resetSurvey: (surveyId: string) => api.post<SurveyResetResult>(`${surveyPath(surveyId)}/reset`),
  /** forma2's generateExampleResponsesAction: AI-written example responses for a survey without any. */
  generateExampleResponses: (surveyId: string) =>
    api.post<ExampleResponsesResult>(`${surveyPath(surveyId)}/ai/example-responses`),
  /** The export file itself, named by the server's Content-Disposition. */
  exportResponses: (surveyId: string, format: ExportFormat, criteria: TResponseFilterCriteria) =>
    downloadFile(`${surveyPath(surveyId)}/responses/export`, { format, filterCriteria: criteria }),
  /** The attachment export's dry run, which the download asks before navigating. */
  attachmentCounts: (surveyId: string, criteria?: TResponseFilterCriteria) =>
    api.get<{ data: AttachmentCounts }>(attachmentsUrl(surveyId, criteria, true)).then((res) => res.data),
  singleUseLinks: (surveyId: string, input: { isEncrypted: boolean; count?: number; singleUseId?: string }) =>
    api
      .post<{ links: SingleUseLink[] }>(`${surveyPath(surveyId)}/single-use-links`, input)
      .then((res) => res.links),
};
