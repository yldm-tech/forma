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
import type { AIUnavailableReason } from "../ai/availability";
import { type SurveyWire, parseSurvey } from "../survey-editor/api";
import type { TFilterSegment } from "./lib/filter-types";

// The analysis views' reads and writes against the Go server: the summary, filter options and displays (apps/server/internal/analysis/handler.go), the responses (apps/server/internal/response/handler.go), tags and the survey's share settings. Timestamps arrive as ISO strings and are revived into Dates here, once, since the views are ported from forma2's components, which read Dates.

/** forma2's RESPONSES_PER_PAGE: the response table loads 25 at a time. */
export const RESPONSES_PER_PAGE = 25;

/** forma2's DISPLAYS_PER_PAGE: the impressions tab loads 15 at a time. */
export const DISPLAYS_PER_PAGE = 15;

/** The filter options of getSurveyFilterDataAction. */
export interface SurveyFilterData {
  environmentTags: TTag[];
  attributes: TSurveyContactAttributes;
  /** The data types (string, number, date) of the attribute keys in attributes; a key the workspace no longer has is missing. */
  attributeTypes: Record<string, string>;
  meta: TSurveyMetaFieldFilter;
  hiddenFields: TResponseHiddenFieldsFilter;
  reservedValues: TSurveyMetaFieldFilter;
  variableValues: TSurveyMetaFieldFilter;
  quotas: TSurveyQuota[];
  /** The workspace's public segments, by title. */
  segments: TFilterSegment[];
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

/** What an AI text summary reads: an open-text question's answers, or the values typed into a choice question's Other option. */
export type TextSummaryTarget = "answers" | "other";

export type TextSentimentLabel = "positive" | "neutral" | "negative" | "mixed";

/** One theme of an AI text summary: how many of the sampled answers it covers, their share in percent, and answers that represent it with the response each came from. */
export interface TextSummaryTheme {
  title: string;
  description: string;
  count: number;
  percentage: number;
  quotes: { responseId: string; text: string }[];
}

/** The AI summary of a question's free-text answers under one filter (apps/server/internal/analysis/text_summary.go). Themes are counted over sampledCount of answerCount answers; readLimit is how many of the newest responses were read when more matched, 0 when all were. */
export interface TextSummary {
  elementId: string;
  target: TextSummaryTarget;
  language: string;
  generatedAt: string;
  answerCount: number;
  sampledCount: number;
  readLimit: number;
  sentiment: { overall: TextSentimentLabel; positive: number; neutral: number; negative: number };
  themes: TextSummaryTheme[];
}

/** Whether the reader may generate a summary, why not when they may not (AI unavailable to the organization, or read_only for a member without edit access, who still gets the current summary), and the current summary, null when there is none. */
export interface TextSummaryState {
  available: boolean;
  reason?: AIUnavailableReason;
  summary: TextSummary | null;
}

export interface TextSummaryRequest {
  elementId: string;
  target: TextSummaryTarget;
  /** The language the themes are written in, the reader's UI language. */
  language: string;
  filterCriteria: TResponseFilterCriteria;
}

/** A tag added to or removed from a response. */
export interface ResponseTagChange {
  responseId: string;
  tagId: string;
}

/** POST /api/app/surveys/{id}/responses/tags/{add,remove}: how many of the survey's responses matched the filter when the change ran, and how many of them gained or lost the tag. */
export interface BulkTagResult {
  tagId: string;
  matched: number;
  changed: number;
}

export type BulkTagOperation = "add" | "remove";

/** The server's bound on the response ids of one bulk tag change (MaxBulkTagResponseIDs in apps/server/internal/response/bulk_tags.go). */
export const BULK_TAG_MAX_RESPONSE_IDS = 1000;

/** One link of POST /api/app/surveys/{id}/single-use-links. */
export interface SingleUseLink {
  suId: string;
  suToken?: string;
}

export type ExportFormat = "csv" | "xlsx";

/** The length of one trend bucket; the server picks one from the range when none is asked for. */
export type TrendInterval = "day" | "week" | "month";

/** What an element trend's values are: the NPS score (-100 to 100), the satisfied share (4 or 5) in percent, or the average answer. */
export type TrendMetric = "nps" | "satisfied" | "average";

/** One trend bucket: its first and last day (local dates in the trend's time zone, both inclusive, cut to the range at the two ends), the responses started in it and how many of them are completed. */
export interface TrendBucket {
  start: string;
  end: string;
  /** The responses read cover only part of the period: the filter's window starts or ends inside it, or it has not ended yet, so its counts are not comparable with a whole period's. */
  partial: boolean;
  responses: number;
  completed: number;
  completionPercentage: number;
}

/** An element's metric in one bucket; value is null when nobody answered it with a number. */
export interface TrendPoint {
  answered: number;
  value: number | null;
}

/** One NPS, CSAT, rating or CES element's metric per bucket, its points lined up with the buckets. */
export interface ElementTrend {
  elementId: string;
  type: string;
  headline: string;
  metric: TrendMetric;
  points: TrendPoint[];
}

/** POST /api/app/surveys/{id}/trends (apps/server/internal/analysis/trends.go): the responses matching the filter by day, ISO week or month in the organization's display time zone. */
export interface SurveyTrends {
  interval: TrendInterval;
  timeZone: string;
  buckets: TrendBucket[];
  elements: ElementTrend[];
}

/** The error code of a trend whose range needs too many buckets at the interval asked for. */
export const TREND_TOO_MANY_BUCKETS = "trend_too_many_buckets";

/** The dry run of the attachment export: how many files the archive would hold, and whether that is over its limit. */
export interface AttachmentCounts {
  fileCount: number;
  responseCount: number;
  exceedsMaxFiles: boolean;
  maxFiles: number;
}

/** forma2's attachment export route (apps/server/internal/analysis/attachments_handler.go), outside /api/app: the browser downloads the ZIP by navigating to it, since an archive of several GB cannot be buffered in the tab. Without criteria it exports every response's files; with dryRun it only counts them. */
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
export const parseSummary = (raw: TSurveySummary): TSurveySummary => ({
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
  trends: (surveyId: string, criteria: TResponseFilterCriteria, interval: TrendInterval | undefined) =>
    [...analysisKeys.all(surveyId), "trends", criteria, interval ?? "auto"] as const,
  filterData: (surveyId: string) => [...analysisKeys.all(surveyId), "filter-data"] as const,
  displays: (surveyId: string) => [...analysisKeys.all(surveyId), "displays"] as const,
  responses: (surveyId: string, criteria: TResponseFilterCriteria) =>
    [...analysisKeys.all(surveyId), "responses", criteria] as const,
  responseCount: (surveyId: string, criteria: TResponseFilterCriteria) =>
    [...analysisKeys.all(surveyId), "response-count", criteria] as const,
  liveResponseCount: (surveyId: string) => [...analysisKeys.all(surveyId), "live-response-count"] as const,
  textSummary: (surveyId: string, request: TextSummaryRequest) =>
    [...analysisKeys.all(surveyId), "text-summary", request] as const,
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

/** The trend under the current filter; without an interval the server picks one from the range and says which. */
export const surveyTrendsQuery = (
  surveyId: string,
  criteria: TResponseFilterCriteria,
  interval: TrendInterval | undefined
) =>
  queryOptions({
    queryKey: analysisKeys.trends(surveyId, criteria, interval),
    queryFn: ({ signal }) =>
      api.post<SurveyTrends>(
        `${surveyPath(surveyId)}/trends`,
        { filterCriteria: criteria, interval },
        { signal }
      ),
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

/** The cached AI summary of a question's free-text answers, with whether AI is available; reading it never calls the AI provider. */
export const textSummaryQuery = (surveyId: string, request: TextSummaryRequest) =>
  queryOptions({
    queryKey: analysisKeys.textSummary(surveyId, request),
    queryFn: ({ signal }) =>
      api.post<TextSummaryState>(`${surveyPath(surveyId)}/text-summaries/lookup`, request, { signal }),
  });

/** How often the analysis of a survey in progress asks whether responses arrived, while its tab is visible. */
export const NEW_RESPONSES_POLL_MS = 20_000;

/** The survey's unfiltered response count, read again every NEW_RESPONSES_POLL_MS except in a background tab. A cache of its own: the count the page was loaded with (surveyResponseCountQuery) stays put, so the difference is what arrived since, and a refresh of the analysis, which invalidates both, brings them level. */
export const surveyLiveResponseCountQuery = (surveyId: string) =>
  queryOptions({
    queryKey: analysisKeys.liveResponseCount(surveyId),
    queryFn: ({ signal }) =>
      api
        .post<{ count: number }>(
          `${surveyPath(surveyId)}/responses/count`,
          { filterCriteria: {} },
          { signal }
        )
        .then((res) => res.count),
    refetchInterval: NEW_RESPONSES_POLL_MS,
    refetchIntervalInBackground: false,
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
  /** Adds the tag to, or removes it from, every response of the survey matching the criteria: a selection is its responseIds (at most BULK_TAG_MAX_RESPONSE_IDS), "all matching" the view's filter. */
  bulkTag: (
    surveyId: string,
    operation: BulkTagOperation,
    tagId: string,
    criteria: TResponseFilterCriteria
  ) =>
    api.post<BulkTagResult>(`${surveyPath(surveyId)}/responses/tags/${operation}`, {
      tagId,
      filterCriteria: criteria,
    }),
  createTag: (workspaceId: string, name: string) =>
    api
      .post<Stamped<TTag>>(`/api/app/workspaces/${workspaceId}/tags`, { name })
      .then((tag) => stamped<TTag>(tag)),
  resetSurvey: (surveyId: string) => api.post<SurveyResetResult>(`${surveyPath(surveyId)}/reset`),
  /** forma2's generateExampleResponsesAction: AI-written example responses for a survey without any. */
  generateExampleResponses: (surveyId: string) =>
    api.post<ExampleResponsesResult>(`${surveyPath(surveyId)}/ai/example-responses`),
  /** Summarizes the answers with the organization's AI; the cached summary when it is current, unless refresh asks for a new one. */
  generateTextSummary: (surveyId: string, request: TextSummaryRequest, refresh: boolean) =>
    api.post<TextSummaryState>(`${surveyPath(surveyId)}/text-summaries`, { ...request, refresh }),
  /** The export file itself, named by the server's Content-Disposition. */
  exportResponses: (surveyId: string, format: ExportFormat, criteria: TResponseFilterCriteria) =>
    downloadFile(`${surveyPath(surveyId)}/responses/export`, { format, filterCriteria: criteria }),
  /** The trend as a file, a row per bucket, named by the server's Content-Disposition. */
  exportTrends: (
    surveyId: string,
    format: ExportFormat,
    criteria: TResponseFilterCriteria,
    interval: TrendInterval | undefined
  ) => downloadFile(`${surveyPath(surveyId)}/trends/export`, { format, filterCriteria: criteria, interval }),
  /** The attachment export's dry run, which the download asks before navigating. */
  attachmentCounts: (surveyId: string, criteria?: TResponseFilterCriteria) =>
    api.get<{ data: AttachmentCounts }>(attachmentsUrl(surveyId, criteria, true)).then((res) => res.data),
  singleUseLinks: (surveyId: string, input: { isEncrypted: boolean; count?: number; singleUseId?: string }) =>
    api
      .post<{ links: SingleUseLink[] }>(`${surveyPath(surveyId)}/single-use-links`, input)
      .then((res) => res.links),
};
