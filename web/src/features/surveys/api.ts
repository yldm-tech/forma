import { queryOptions } from "@tanstack/react-query";
import { api } from "../../lib/api";
import { type SurveyFilters, type SurveyStatus, normalizeSurveyFilters } from "./list/filters";

// The survey endpoints the list, the templates page and the onboarding flow call: /api/v3/surveys (docs/api-v3-reference, server/internal/survey/v3ops.go) and the /api/app survey routes (server/internal/survey/handler.go).

/** One row of GET /api/v3/surveys. Timestamps are ISO strings. */
export interface SurveyListItem {
  id: string;
  name: string;
  workspaceId: string;
  type: string;
  status: SurveyStatus;
  publishOn: string | null;
  archivedAt: string | null;
  createdAt: string;
  updatedAt: string;
  responseCount: number;
  completedResponseCount: number;
  creator: { name: string } | null;
}

export interface SurveyListPage {
  data: SurveyListItem[];
  meta: {
    limit: number;
    nextCursor: string | null;
    /** Surveys matching the filter; null on every page after the first, which asks without counts. */
    totalCount: number | null;
    /** Every survey of the workspace, archived ones included, whatever the filter: it tells an empty workspace from an empty result. Null like totalCount. */
    workspaceSurveyCount: number | null;
  };
}

/** The acknowledgement of an archive or a restore. */
export interface SurveyLifecycle {
  id: string;
  status: SurveyStatus;
  archivedAt: string | null;
}

/** forma2's getV3ApiErrorMessage: a v3 problem's detail, any other error's own message, else the fallback. */
export const v3ErrorMessage = (error: unknown, fallback: string): string => {
  if (error instanceof Error && error.message) return error.message;
  return fallback;
};

/** forma2's buildSurveyListSearchParams. Pages after the first ask without counts, which only page one carries. */
export const surveyListSearchParams = ({
  workspaceId,
  limit,
  cursor,
  includeTotalCount,
  filters,
}: {
  workspaceId: string;
  limit: number;
  cursor?: string | null;
  includeTotalCount?: boolean;
  filters: SurveyFilters;
}): URLSearchParams => {
  const normalized = normalizeSurveyFilters(filters);
  const params = new URLSearchParams();
  params.set("workspaceId", workspaceId);
  params.set("limit", String(limit));
  params.set("sortBy", normalized.sortBy);
  if (cursor) params.set("cursor", cursor);
  if (includeTotalCount === false) params.set("includeTotalCount", "false");
  if (normalized.name) params.set("filter[name][contains]", normalized.name);
  for (const status of normalized.status) params.append("filter[status][in]", status);
  for (const type of normalized.type) params.append("filter[type][in]", type);
  return params;
};

export const surveysApi = {
  list: (input: Parameters<typeof surveyListSearchParams>[0], signal?: AbortSignal) =>
    api.get<SurveyListPage>(`/api/v3/surveys?${surveyListSearchParams(input).toString()}`, { signal }),
  updateStatus: (surveyId: string, status: SurveyStatus) =>
    api.patch<{ data: { id: string; status: SurveyStatus } }>(`/api/v3/surveys/${surveyId}`, { status }),
  rename: (surveyId: string, name: string) =>
    api.patch<{ data: { id: string; name: string } }>(`/api/v3/surveys/${surveyId}`, { name }),
  delete: (surveyId: string) => api.delete<void>(`/api/v3/surveys/${surveyId}`),
  archive: (surveyId: string) =>
    api.post<{ data: SurveyLifecycle }>(`/api/v3/surveys/${surveyId}/archive`).then((res) => res.data),
  restore: (surveyId: string) =>
    api.post<{ data: SurveyLifecycle }>(`/api/v3/surveys/${surveyId}/restore`).then((res) => res.data),
  /** forma2's copySurveyToOtherWorkspaceAction; a target equal to the survey's own workspace duplicates it. */
  copy: (surveyId: string, targetWorkspaceId: string) =>
    api.post<{ id: string }>(`/api/app/surveys/${surveyId}/copy`, { targetWorkspaceId }),
};

export type TemplateSource = "catalog" | "custom" | "xm";

export interface CreateFromTemplateInput {
  workspaceId: string;
  templateId: string;
  source: TemplateSource;
  surveyType: "link" | "app";
  /** The survey's default language (resolveDefaultSurveyLanguage), not the creator's dashboard locale. */
  defaultLanguage: string;
}

/** forma2's CUSTOM_SURVEY_TEMPLATE_ID: "start from scratch". */
export const CUSTOM_TEMPLATE_ID = "custom";

/** forma2's createSurveyFromTemplate: POST /api/v3/surveys/templates, which answers the created survey. */
export const createSurveyFromTemplate = (input: CreateFromTemplateInput) =>
  api.post<{ data: { id: string } }>("/api/v3/surveys/templates", input).then((res) => res.data);

export type TemplateRole = "productManager" | "marketing" | "customerSuccess" | "peopleManager" | "sales";
export type TemplateChannel = "website" | "app" | "link";
export type TemplateIndustry = "eCommerce" | "saas" | "other";

/** A catalog template as GET /api/app/workspaces/{id}/survey-templates renders it: translated into the user's dashboard language, with the workspace's name filled in. Only the parts the cards read are typed. */
export interface SurveyTemplate {
  id: string;
  name: string;
  description: string;
  role?: TemplateRole;
  channels?: TemplateChannel[];
  industries?: TemplateIndustry[];
  preset: { blocks?: { logic?: unknown[] }[] };
}

export const surveyTemplatesQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: ["workspaces", workspaceId, "survey-templates"],
    queryFn: ({ signal }) =>
      api.get<{ templates: SurveyTemplate[]; xmTemplateIds: string[] }>(
        `/api/app/workspaces/${workspaceId}/survey-templates`,
        { signal }
      ),
    staleTime: Number.POSITIVE_INFINITY,
  });

/** The origin survey links are built on (forma2's getPublicDomain). */
export const publicDomainQuery = queryOptions({
  queryKey: ["public-domain"],
  queryFn: ({ signal }) =>
    api.get<{ publicDomain: string }>("/api/app/public-domain", { signal }).then((res) => res.publicDomain),
  staleTime: Number.POSITIVE_INFINITY,
});
