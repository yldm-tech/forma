import type { TSurvey } from "@forma/types/surveys/types";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../../../../lib/api";
import { type SurveyWire, forgetEditorSurvey, parseSurvey, saveSurvey } from "../../../survey-editor/api";
import { surveyKeys } from "../../../surveys/list/query";
import { analysisKeys } from "../../api";

/** The stored survey after a change made from the share modal: the analysis views show it, the editor reloads it next time, and the lists refetch. */
const useAdoptSurvey = () => {
  const queryClient = useQueryClient();
  return (survey: TSurvey) => {
    queryClient.setQueryData(analysisKeys.survey(survey.id), survey);
    forgetEditorSurvey(queryClient, survey.id);
    void queryClient.invalidateQueries({ queryKey: surveyKeys.lists() });
  };
};

/** forma2's updateSurveyAction from the share settings: the survey saved whole with one part changed, as the published survey it is. */
export const useUpdateSurveySettings = (survey: TSurvey) => {
  const adopt = useAdoptSurvey();
  return useMutation({
    mutationFn: (patch: Partial<TSurvey>) => saveSurvey({ ...survey, ...patch }, false),
    onSuccess: adopt,
  });
};

/** forma2's updateSurveySlugAction and removeSurveySlugAction: the survey's pretty URL, /p/<slug>. */
export const useSurveySlug = (surveyId: string) => {
  const adopt = useAdoptSurvey();
  return useMutation({
    mutationFn: (slug: string | null) =>
      (slug === null
        ? api.delete<SurveyWire>(`/api/app/surveys/${surveyId}/slug`)
        : api.put<SurveyWire>(`/api/app/surveys/${surveyId}/slug`, { slug })
      ).then(parseSurvey),
    onSuccess: adopt,
  });
};
