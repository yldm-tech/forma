import { z } from "zod";
import { ZActionClass } from "@forma/types/action-classes";
import { ZSegment } from "@forma/types/segment";
import { ZSurveyFollowUp } from "@forma/types/surveys/follow-up";
import { ZSurveyBase, ZSurveyLanguage, surveyRefinement } from "@forma/types/surveys/types";
import { ZLanguage } from "@forma/types/workspace";

// The ids of records the server creates (the survey, its workspace, action classes, segments, languages, follow-ups) are UUIDs on the Go server where forma2's were cuid2. Everything the editor itself creates (blocks, endings, variables, logic) keeps forma2's id rules.
const ZServerId = z.string().min(1);

/** forma2's ZSurvey, refinements included, accepting the Go server's record ids. */
export const ZEditorSurvey = ZSurveyBase.extend({
  id: ZServerId,
  workspaceId: ZServerId,
  triggers: z.array(
    z.object({ actionClass: ZActionClass.extend({ id: ZServerId, workspaceId: ZServerId }) })
  ),
  segment: ZSegment.extend({
    workspaceId: ZServerId,
    surveys: z.array(ZServerId),
  }).nullable(),
  languages: z.array(
    ZSurveyLanguage.extend({ language: ZLanguage.extend({ id: ZServerId, workspaceId: ZServerId }) })
  ),
  followUps: z.array(
    ZSurveyFollowUp.extend({ id: ZServerId, surveyId: ZServerId, deleted: z.boolean().optional() })
  ),
}).superRefine(surveyRefinement);
