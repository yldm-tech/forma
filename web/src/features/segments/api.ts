import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import type { TSegment, TSegmentCreateInput, TSegmentUpdateInput } from "@forma/types/segment";
import { api } from "../../lib/api";
import { type Stamped } from "../../lib/wire";
import { type SurveyWire, parseSurvey } from "../survey-editor/api";

// The contact segments the survey editor's targeting card reads and writes. forma2 named these segment actions; they are /api/app endpoints of the contact module here.

/** A segment as the server lists it, with the surveys that target it split by whether they are running. */
export type SegmentWithSurveys = TSegment & {
  activeSurveys: { id: string; name: string }[];
  inactiveSurveys: { id: string; name: string }[];
};

type SegmentWire = Stamped<SegmentWithSurveys>;

const parseSegment = (segment: SegmentWire): SegmentWithSurveys => ({
  ...segment,
  createdAt: new Date(segment.createdAt),
  updatedAt: new Date(segment.updatedAt),
});

/** A survey as the survey-interaction filter offers it. */
export interface SegmentSurveyRef {
  id: string;
  name: string;
  status: string;
}

export const segmentKeys = {
  list: (workspaceId: string) => ["workspaces", workspaceId, "segments"] as const,
  surveys: (workspaceId: string) => ["workspaces", workspaceId, "segment-surveys"] as const,
  attributeValues: (keyId: string) => ["contact-attribute-keys", keyId, "values"] as const,
};

export const segmentsQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: segmentKeys.list(workspaceId),
    queryFn: ({ signal }) =>
      api
        .get<SegmentWire[]>(`/api/app/workspaces/${workspaceId}/segments`, { signal })
        .then((segments) => segments.map(parseSegment)),
  });

/** The surveys a survey-interaction filter can name (at most 1000). */
export const segmentSurveysQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: segmentKeys.surveys(workspaceId),
    queryFn: ({ signal }) =>
      api.get<SegmentSurveyRef[]>(`/api/app/workspaces/${workspaceId}/segment-surveys`, { signal }),
  });

/** Up to 100 distinct values contacts have for an attribute key, to suggest in a filter's value field. */
export const attributeValuesQuery = (keyId: string) =>
  queryOptions({
    queryKey: segmentKeys.attributeValues(keyId),
    queryFn: ({ signal }) => api.get<string[]>(`/api/app/contact-attribute-keys/${keyId}/values`, { signal }),
  });

/** forma2's TSegmentCreateInput; the workspace comes from the path. */
export type SegmentCreateInput = Omit<TSegmentCreateInput, "workspaceId">;

const useSegmentMutation = <TInput, TResult>(
  workspaceId: string,
  mutationFn: (input: TInput) => Promise<TResult>
) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: segmentKeys.list(workspaceId) }),
  });
};

export const useCreateSegment = (workspaceId: string) =>
  useSegmentMutation(workspaceId, (input: SegmentCreateInput) =>
    api.post<SegmentWire>(`/api/app/workspaces/${workspaceId}/segments`, input).then(parseSegment)
  );

export const useUpdateSegment = (workspaceId: string) =>
  useSegmentMutation(workspaceId, ({ segmentId, data }: { segmentId: string; data: TSegmentUpdateInput }) =>
    api.patch<SegmentWire>(`/api/app/segments/${segmentId}`, data).then(parseSegment)
  );

/** Copies a segment as "Copy of X (n)" targeted by the survey. */
export const useCloneSegment = (workspaceId: string) =>
  useSegmentMutation(workspaceId, ({ segmentId, surveyId }: { segmentId: string; surveyId: string }) =>
    api.post<SegmentWire>(`/api/app/segments/${segmentId}/clone`, { surveyId }).then(parseSegment)
  );

/** Empties the survey's private segment, creating it when the survey has none. */
export const useResetSurveySegment = (workspaceId: string) =>
  useSegmentMutation(workspaceId, (surveyId: string) =>
    api.post<SegmentWire>(`/api/app/surveys/${surveyId}/segment/reset`).then(parseSegment)
  );

/** Points the survey at another segment of the workspace, deleting the private one it leaves behind, and returns the survey. */
export const useLoadSegment = (workspaceId: string) =>
  useSegmentMutation(workspaceId, ({ surveyId, segmentId }: { surveyId: string; segmentId: string }) =>
    api.put<SurveyWire>(`/api/app/surveys/${surveyId}/segment`, { segmentId }).then(parseSurvey)
  );
