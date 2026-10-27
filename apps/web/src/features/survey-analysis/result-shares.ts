import { type QueryClient, queryOptions } from "@tanstack/react-query";
import type { TResponseFilterCriteria } from "@forma/types/responses";
import type { TSurvey, TSurveySummary } from "@forma/types/surveys/types";
import { api } from "../../lib/api";
import { appHref } from "../../lib/base-path";
import { templatePreviewSurvey } from "../surveys/templates/preview";
import { type SurveyTrends, type TrendInterval, analysisKeys, parseSummary } from "./api";

// A survey's read-only results links (apps/server/internal/analysis/shares.go): managed from the summary by whoever may publish the survey, and read without an account at /results?token=<key>, where the summary is computed under the filter the link froze. The key travels in the request body, never in an API path.

/** Who created a link, while their account exists. */
export interface ResultShareCreator {
  id: string;
  name: string;
}

/** A results link as the people who manage it see it. */
export interface ResultShare {
  id: string;
  createdAt: string;
  updatedAt: string;
  surveyId: string;
  /** The link's secret; the link is /results?token=<key>. */
  key: string;
  name: string;
  /** The filter the link shows the summary under, frozen when it was created. */
  filterCriteria: TResponseFilterCriteria;
  /** The cards listing individual answers that the link shows; every other such card shows only its counts. */
  revealedElementIds: string[];
  expiresAt: string | null;
  creator: ResultShareCreator | null;
  /** The segments the frozen filter names that no longer exist, or that use one that no longer exists: the link no longer shows the responses it was created for. Absent from an older server's answer. */
  missingSegmentIds?: string[];
}

/** What a card that lists individual answers shows: an element's answers, the values typed into a choice element's Other option, or a hidden field's values. */
export type RevealKind = "answers" | "otherValues" | "hiddenField";

/** A card of the survey a link hides unless it reveals the card's id. */
export interface Revealable {
  id: string;
  kind: RevealKind;
  /** The element's type; missing for a hidden field. */
  type?: string;
}

/** GET /api/app/surveys/{id}/result-shares: the survey's links, newest first, and the cards a link can reveal. */
export interface ResultShares {
  shares: ResultShare[];
  revealable: Revealable[];
}

/** A new link: its name, the filter it freezes, the cards it reveals, and when it stops working (null for never). */
export interface ResultShareInput {
  name: string;
  filterCriteria: TResponseFilterCriteria;
  revealedElementIds: string[];
  expiresAt: string | null;
}

/** What may change of a link after it is created; its filter stays frozen. */
export type ResultShareUpdate = Omit<ResultShareInput, "filterCriteria">;

/** The parts of the survey document a link shows: what the summary cards read for headlines, choices and recall. */
export interface SharedSurveyWire {
  id: string;
  name: string;
  type: TSurvey["type"];
  blocks: TSurvey["blocks"];
  endings: TSurvey["endings"];
  hiddenFields: TSurvey["hiddenFields"];
  variables: TSurvey["variables"];
}

/** What a link says about itself to its readers. */
export interface SharedResultsLink {
  name: string;
  expiresAt: string | null;
  /** The link shows only the responses matching a filter. */
  filtered: boolean;
  /** The filter names a segment that no longer exists, so the responses shown are not the ones the link was created for. */
  segmentMissing: boolean;
}

/** POST /api/app/shared-results/summary as the page reads it: the link, the survey as the summary cards take it, the summary, and the cards whose individual answers the link does not show. */
export interface SharedResults {
  link: SharedResultsLink;
  survey: TSurvey;
  summary: TSurveySummary;
  withheldIds: ReadonlySet<string>;
}

interface SharedResultsWire {
  link: SharedResultsLink;
  survey: SharedSurveyWire;
  summary: TSurveySummary;
  withheldIds: string[];
}

/** The error code of a key that names no link, an expired one or a revoked one. */
export const RESULT_SHARE_NOT_FOUND = "result_share_not_found";

/** The error code of a new link whose filter names a segment the workspace does not have (one deleted since the filter was set). */
export const RESULT_SHARE_SEGMENT_NOT_FOUND = "result_share_segment_not_found";

/** The address of a link: the app's /results page with the key as its token, a query parameter telemetry masks. */
export const resultShareUrl = (origin: string, key: string) =>
  `${origin}${appHref(`/results?token=${encodeURIComponent(key)}`)}`;

/** Whether a link has stopped working. */
export const isResultShareExpired = (share: Pick<ResultShare, "expiresAt">, now: Date = new Date()) =>
  share.expiresAt !== null && new Date(share.expiresAt).getTime() <= now.getTime();

/** The survey a link shows, as the summary cards take a survey: the parts the server sent, and a new survey's values for everything else, none of which the cards read. */
const sharedSurvey = (wire: SharedSurveyWire): TSurvey => ({
  ...templatePreviewSurvey(
    { id: wire.id, name: wire.name, description: "", preset: {} },
    wire.type === "app" ? "app" : "link"
  ),
  blocks: wire.blocks,
  endings: wire.endings,
  hiddenFields: wire.hiddenFields,
  variables: wire.variables,
});

/** The summary a link shows has no response ids; its samples get keys of their own, by position, which is all the cards key them by. */
const keyedSamples = (summary: TSurveySummary): TSurveySummary => ({
  ...summary,
  summary: summary.summary.map((entry) => {
    if ("samples" in entry && entry.type !== "hiddenField") {
      return {
        ...entry,
        samples: entry.samples.map((sample, index) => ({ ...sample, id: `${entry.element.id}-${index}` })),
      } as typeof entry;
    }
    if ("files" in entry) {
      return {
        ...entry,
        files: entry.files.map((file, index) => ({ ...file, id: `${entry.element.id}-${index}` })),
      } as typeof entry;
    }
    return entry;
  }),
});

export const resultShareKeys = {
  list: (surveyId: string) => [...analysisKeys.all(surveyId), "result-shares"] as const,
  shared: (token: string) => ["shared-results", token] as const,
  sharedTrends: (token: string, interval: TrendInterval | undefined) =>
    ["shared-results", token, "trends", interval ?? "auto"] as const,
};

/** The survey's links, for those who may publish it. */
export const resultSharesQuery = (surveyId: string) =>
  queryOptions({
    queryKey: resultShareKeys.list(surveyId),
    queryFn: ({ signal }) => api.get<ResultShares>(`/api/app/surveys/${surveyId}/result-shares`, { signal }),
  });

/** The summary a link shows, read through its key. A link that stops working answers RESULT_SHARE_NOT_FOUND, which is final, so it is not retried. */
export const sharedResultsQuery = (token: string) =>
  queryOptions({
    queryKey: resultShareKeys.shared(token),
    queryFn: ({ signal }) =>
      api
        .post<SharedResultsWire>("/api/app/shared-results/summary", { token }, { signal })
        .then((raw): SharedResults => ({
          link: raw.link,
          survey: sharedSurvey(raw.survey),
          summary: keyedSamples(parseSummary(raw.summary)),
          withheldIds: new Set(raw.withheldIds),
        })),
    retry: false,
  });

/** The trend a link shows at an interval, read through its key under the link's filter. */
export const sharedTrendsQuery = (token: string, interval: TrendInterval | undefined) =>
  queryOptions({
    queryKey: resultShareKeys.sharedTrends(token, interval),
    queryFn: ({ signal }) =>
      api.post<SurveyTrends>("/api/app/shared-results/trends", { token, interval }, { signal }),
    retry: false,
  });

export const resultSharesApi = {
  create: (surveyId: string, input: ResultShareInput) =>
    api.post<ResultShare>(`/api/app/surveys/${surveyId}/result-shares`, input),
  update: (surveyId: string, shareId: string, input: ResultShareUpdate) =>
    api.put<ResultShare>(`/api/app/surveys/${surveyId}/result-shares/${shareId}`, input),
  revoke: (surveyId: string, shareId: string) =>
    api.delete<void>(`/api/app/surveys/${surveyId}/result-shares/${shareId}`),
};

/** Reads the survey's links again after one changed. */
export const invalidateResultShares = (queryClient: QueryClient, surveyId: string) =>
  queryClient.invalidateQueries({ queryKey: resultShareKeys.list(surveyId) });
