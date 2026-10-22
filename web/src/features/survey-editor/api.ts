import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import type { TActionClass } from "@forma/types/action-classes";
import type { TContactAttributeKey } from "@forma/types/contact-attribute-key";
import type { TSurveyQuota, TSurveyQuotaInput } from "@forma/types/quota";
import type { TSegment } from "@forma/types/segment";
import type { TSurvey, TSurveyLanguage } from "@forma/types/surveys/types";
import type { TLanguage, TWorkspace } from "@forma/types/workspace";
import { ApiError, api } from "../../lib/api";
import type { Member } from "../../lib/queries/organizations";
import type { TeamDetails } from "../../lib/queries/teams";
import { type Stamped, optionalDate, stamped } from "../../lib/wire";
import { parseActionClass } from "../action-classes/api";
import type { WorkspaceDetail } from "../../lib/queries/workspaces";
import type { TFollowUpEmailToUser } from "./types/survey-follow-up";

// The editor's reads and writes against the Go server. The server sends timestamps as ISO strings; the editor works on forma2's types, whose timestamps are Dates, so every read revives them here, once, at the boundary.

export const parseLanguage = (language: Stamped<TLanguage>): TLanguage => stamped<TLanguage>(language);

type FollowUp = TSurvey["followUps"][number];

/** A survey as GET /api/app/surveys/{id} sends it. */
export type SurveyWire = Omit<
  TSurvey,
  | "createdAt"
  | "updatedAt"
  | "publishOn"
  | "closeOn"
  | "archivedAt"
  | "questions"
  | "triggers"
  | "segment"
  | "languages"
  | "followUps"
> & {
  createdAt: string;
  updatedAt: string;
  publishOn: string | null;
  closeOn: string | null;
  archivedAt?: string | null;
  triggers: { actionClass: Stamped<TActionClass> }[];
  segment: Stamped<TSegment> | null;
  languages: (Omit<TSurveyLanguage, "language"> & { language: Stamped<TLanguage> })[];
  followUps: Stamped<FollowUp>[];
};

/** A survey as forma2's TSurvey. Blocks are the only document model the Go server stores, so the legacy questions list is always empty. */
export const parseSurvey = (raw: SurveyWire): TSurvey => ({
  ...raw,
  createdAt: new Date(raw.createdAt),
  updatedAt: new Date(raw.updatedAt),
  publishOn: optionalDate(raw.publishOn) ?? null,
  closeOn: optionalDate(raw.closeOn) ?? null,
  archivedAt: optionalDate(raw.archivedAt),
  questions: [],
  triggers: raw.triggers.map((trigger) => ({ actionClass: parseActionClass(trigger.actionClass) })),
  segment: raw.segment ? stamped<TSegment>(raw.segment) : null,
  languages: raw.languages.map((language) => ({ ...language, language: parseLanguage(language.language) })),
  followUps: raw.followUps.map((followUp) => stamped<FollowUp>(followUp)),
});

/** The workspace detail as forma2's TWorkspace, which the editor's cards and the preview read. */
export const toEditorWorkspace = ({ access: _access, ...detail }: WorkspaceDetail): TWorkspace => ({
  ...detail,
  createdAt: new Date(detail.createdAt),
  updatedAt: new Date(detail.updatedAt),
  languages: detail.languages.map(parseLanguage),
});

export const surveyEditorKeys = {
  survey: (surveyId: string) => ["surveys", surveyId, "editor"] as const,
  quotas: (surveyId: string) => ["surveys", surveyId, "quotas"] as const,
  responseCounts: (surveyId: string) => ["surveys", surveyId, "response-counts"] as const,
  attributeKeys: (workspaceId: string) => ["workspaces", workspaceId, "contact-attribute-keys"] as const,
};

/**
 * The survey being edited. It is the editor's baseline, the version its unsaved-changes check and its draft save conflict check compare against, so it changes only when the editor itself saves: never on a window focus or a stale timer, which would quietly move the baseline past another tab's save and let an autosave overwrite it.
 */
export const editorSurveyQuery = (surveyId: string) =>
  queryOptions({
    queryKey: surveyEditorKeys.survey(surveyId),
    queryFn: ({ signal }) =>
      api.get<SurveyWire>(`/api/app/surveys/${surveyId}`, { signal }).then(parseSurvey),
    staleTime: Number.POSITIVE_INFINITY,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });

/** A survey as the editor sends it: forma2's TSurvey, with updatedAt as the version the save is based on. */
export type SurveySaveInput = Omit<TSurvey, "updatedAt"> & { updatedAt: Date | null };

/** The editor's full save (forma2's updateSurveyAction) and, with draft, its draft save (updateSurveyDraftAction), which the server refuses with survey_modified_elsewhere:<iso> when the stored survey is newer than updatedAt. */
export const saveSurvey = (input: SurveySaveInput, draft: boolean): Promise<TSurvey> =>
  api.put<SurveyWire>(`/api/app/surveys/${input.id}${draft ? "/draft" : ""}`, input).then(parseSurvey);

/** forma2's response counts of the survey (all, and finished ones), read from its row in the workspace's survey list. */
export interface ResponseCounts {
  responseCount: number;
  finishedResponseCount: number;
}

interface SurveyListPage {
  data: { id: string; responseCount: number; completedResponseCount: number }[];
  meta: { nextCursor: string | null };
}

const findResponseCounts = async (
  workspaceId: string,
  surveyId: string,
  name: string,
  signal: AbortSignal
): Promise<ResponseCounts> => {
  let cursor: string | null = null;
  do {
    const params = new URLSearchParams({
      workspaceId,
      limit: "250",
      includeTotalCount: "false",
      "filter[name][contains]": name,
    });
    params.append("filter[status][in]", "draft,inProgress,paused,completed,archived");
    if (cursor) params.set("cursor", cursor);
    const page: SurveyListPage = await api.get<SurveyListPage>(`/api/v3/surveys?${params.toString()}`, {
      signal,
    });
    const row = page.data.find((item) => item.id === surveyId);
    if (row) return { responseCount: row.responseCount, finishedResponseCount: row.completedResponseCount };
    cursor = page.meta.nextCursor;
  } while (cursor);
  return { responseCount: 0, finishedResponseCount: 0 };
};

export const responseCountsQuery = (workspaceId: string, surveyId: string, name: string) =>
  queryOptions({
    queryKey: surveyEditorKeys.responseCounts(surveyId),
    queryFn: ({ signal }) => findResponseCounts(workspaceId, surveyId, name, signal),
  });

export const contactAttributeKeysQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: surveyEditorKeys.attributeKeys(workspaceId),
    queryFn: ({ signal }) =>
      api
        .get<Stamped<TContactAttributeKey>[]>(`/api/app/workspaces/${workspaceId}/contact-attribute-keys`, {
          signal,
        })
        .then((keys) => keys.map((key) => stamped<TContactAttributeKey>(key))),
  });

const parseQuota = (quota: Stamped<TSurveyQuota>): TSurveyQuota => stamped<TSurveyQuota>(quota);

export const quotasQuery = (surveyId: string) =>
  queryOptions({
    queryKey: surveyEditorKeys.quotas(surveyId),
    queryFn: ({ signal }) =>
      api
        .get<{ quotas: Stamped<TSurveyQuota>[] }>(`/api/app/surveys/${surveyId}/quotas`, { signal })
        .then((res) => res.quotas.map(parseQuota)),
  });

export type QuotaInput = Omit<TSurveyQuotaInput, "surveyId">;

export const useCreateQuota = (surveyId: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: QuotaInput) =>
      api.post<Stamped<TSurveyQuota>>(`/api/app/surveys/${surveyId}/quotas`, input).then(parseQuota),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: surveyEditorKeys.quotas(surveyId) }),
  });
};

export const useUpdateQuota = (surveyId: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: QuotaInput }) =>
      api.patch<Stamped<TSurveyQuota>>(`/api/app/quotas/${id}`, input).then(parseQuota),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: surveyEditorKeys.quotas(surveyId) }),
  });
};

export const useDeleteQuota = (surveyId: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.delete<undefined>(`/api/app/quotas/${id}`),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: surveyEditorKeys.quotas(surveyId) }),
  });
};

/** The responses screened into a quota. */
export const fetchQuotaResponseCount = (quotaId: string): Promise<number> =>
  api.get<{ count: number }>(`/api/app/quotas/${quotaId}/response-count`).then((res) => res.count);

/**
 * forma2's getTeamMemberDetails: the name and email of every member of the teams with access to the workspace, which a follow-up can be addressed to. The emails come from the organization's member list; a caller who may not read it gets no team members, only themselves.
 */
export const followUpTeamMembersQuery = (workspaceId: string, organizationId: string) =>
  queryOptions({
    queryKey: ["workspaces", workspaceId, "follow-up-team-members"],
    queryFn: async ({ signal }): Promise<TFollowUpEmailToUser[]> => {
      const teams = await api.get<{ id: string }[]>(`/api/app/workspaces/${workspaceId}/teams`, { signal });
      if (teams.length === 0) return [];
      const [details, members] = await Promise.all([
        Promise.all(teams.map((team) => api.get<TeamDetails>(`/api/app/teams/${team.id}`, { signal }))),
        api
          .get<{ members: Member[] }>(`/api/app/organizations/${organizationId}/members`, { signal })
          .then((res) => res.members)
          .catch((error: unknown) => {
            if (error instanceof ApiError && error.status === 403) return [];
            throw error;
          }),
      ]);
      const userIds = new Set(details.flatMap((team) => team.members.map((member) => member.userId)));
      return members
        .filter((member) => userIds.has(member.userId))
        .map((member) => ({ name: member.name, email: member.email }));
    },
  });
