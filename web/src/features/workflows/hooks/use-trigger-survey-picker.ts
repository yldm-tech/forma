import { queryOptions, useQuery } from "@tanstack/react-query";
import type { TSurvey, TSurveyEnding } from "@forma/types/surveys/types";
import { getTextContent } from "@forma/types/surveys/validation";
import { api } from "../../../lib/api";
import { recallToHeadline } from "../../../lib/survey/recall";
import { type SurveyWire, parseSurvey } from "../../survey-editor/api";
import { surveysApi } from "../../surveys/api";
import { initialFilters } from "../../surveys/list/filters";

// The trigger's survey picker and the bound survey, forma2's modules/workflows/hooks/use-trigger-survey-picker.ts. forma2 read a survey's endings from its v3 resource; here the survey comes from the same /api/app read the survey editor uses, so endings and the send_email inspector's recall share one fetch.

// The picker needs every survey, so the v3 cursor list is walked until it ends.
const SURVEY_LIST_PAGE_SIZE = 100;
// A ceiling so a runaway cursor cannot spin forever: 2,000 surveys, then the picker truncates.
const SURVEY_LIST_MAX_PAGES = 20;

interface TWorkflowSurveyOption {
  id: string;
  name: string;
}

export interface TWorkflowSurveyEnding {
  id: string;
  label: string;
}

const DEFAULT_LANGUAGE_CODE = "default";

/** What an ending is called in the picker: an end screen's headline with its recall resolved as the editor shows it, a redirect's label, else its id. */
export const endingDisplayLabel = (ending: TSurveyEnding, survey: TSurvey): string => {
  if (ending.type === "endScreen" && ending.headline) {
    const headline = recallToHeadline(ending.headline, survey, false, DEFAULT_LANGUAGE_CODE)[
      DEFAULT_LANGUAGE_CODE
    ];
    const text = headline ? getTextContent(headline) : "";
    if (text) return text;
  } else if (ending.type === "redirectToUrl") {
    const label = ending.label?.trim();
    if (label) return label;
  }
  return ending.id;
};

export const workflowSurveyQuery = (surveyId: string) =>
  queryOptions({
    queryKey: ["workflow-trigger", "survey", surveyId],
    queryFn: ({ signal }) =>
      api.get<SurveyWire>(`/api/app/surveys/${surveyId}`, { signal }).then(parseSurvey),
    retry: false,
  });

export const useWorkflowSurveyOptions = (workspaceId: string) => {
  const query = useQuery({
    queryKey: ["workflow-trigger", "surveys", workspaceId],
    queryFn: async ({ signal }) => {
      const options: TWorkflowSurveyOption[] = [];
      let cursor: string | null = null;
      for (let page = 0; page < SURVEY_LIST_MAX_PAGES; page++) {
        const result = await surveysApi.list(
          {
            workspaceId,
            limit: SURVEY_LIST_PAGE_SIZE,
            cursor,
            includeTotalCount: false,
            filters: initialFilters,
          },
          signal
        );
        for (const survey of result.data) options.push({ id: survey.id, name: survey.name });
        cursor = result.meta.nextCursor;
        if (!cursor) break;
      }
      return options;
    },
  });
  return { ...query, options: query.data ?? [] };
};

/** The survey a trigger names, when it is a survey of this workspace; a survey of another workspace is as good as missing. */
export const useWorkflowSurvey = (surveyId: string | null | undefined, workspaceId: string) => {
  const query = useQuery({ ...workflowSurveyQuery(surveyId ?? ""), enabled: Boolean(surveyId) });
  const survey = query.data && query.data.workspaceId === workspaceId ? query.data : null;
  return { ...query, survey };
};

/** The endings of the survey a trigger names. resolvedSurveyId is the survey the endings belong to: a caller pruning stored ids must check it, since reading a previous survey's or an unsettled answer as the current one would delete a valid selection. */
export const useWorkflowSurveyEndings = (surveyId: string | null | undefined) => {
  const query = useQuery({ ...workflowSurveyQuery(surveyId ?? ""), enabled: Boolean(surveyId) });
  const survey = query.data;
  return {
    ...query,
    endings: survey
      ? survey.endings.map((ending) => ({ id: ending.id, label: endingDisplayLabel(ending, survey) }))
      : [],
    resolvedSurveyId: survey?.id ?? null,
  };
};
