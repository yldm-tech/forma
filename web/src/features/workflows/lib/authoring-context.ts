import type { TSurvey } from "@forma/types/surveys/types";
import type { TUserLocale } from "@forma/types/user";
import type { TWorkflowDefinition } from "../definition";
import type { TFollowUpEmailToUser } from "../../survey-editor/types/survey-follow-up";

/** What the send_email inspector authors an email against, forma2's TWorkflowEmailAuthoringContext. survey is null when no survey of the workspace is bound, or it was deleted. */
export interface TWorkflowEmailAuthoringContext {
  survey: TSurvey | null;
  teamMemberDetails: TFollowUpEmailToUser[];
  userEmail: string;
  mailFrom: string;
  locale: TUserLocale;
}

/** forma2's resolveBoundTriggerSurvey: the survey the trigger is bound to, or null when there is none to author against. The context's survey counts only while its id is the trigger's current surveyId, which also covers a fresh trigger's placeholder id and a deleted survey. */
export const resolveBoundTriggerSurvey = (
  authoringContext: TWorkflowEmailAuthoringContext | null,
  definition: TWorkflowDefinition | null
): TSurvey | null => {
  const triggerSurveyId = definition?.trigger?.type === "trigger" ? definition.trigger.config.surveyId : null;
  const survey = authoringContext?.survey ?? null;
  return survey?.id === triggerSurveyId ? survey : null;
};
