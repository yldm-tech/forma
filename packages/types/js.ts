import { z } from "zod";
import { ZActionClass } from "./action-classes";
import { ZId } from "./common";
import { ZUploadFileConfig } from "./storage";
import { ZSurveyBase, surveyRefinement } from "./surveys/types";
import { ZWorkspace } from "./workspace";

export const ZJsWorkspaceStateSurvey = ZSurveyBase.pick({
  id: true,
  name: true,
  welcomeCard: true,
  questions: true,
  blocks: true,
  variables: true,
  type: true,
  showLanguageSwitch: true,
  languages: true,
  endings: true,
  autoClose: true,
  styling: true,
  status: true,
  segment: true,
  recontactDays: true,
  displayLimit: true,
  displayOption: true,
  hiddenFields: true,
  // The inlined Embedded Data definitions (ENG-1837). Picked so the join survives this parse and
  // reaches the renderer's logic/recall engines, which resolve definitions through it.
  embeddedFields: true,
  triggers: true,
  displayPercentage: true,
  delay: true,
  workspaceOverwrites: true,
  isBackButtonHidden: true,
  isAutoProgressingEnabled: true,
  recaptcha: true,
}).superRefine((survey, ctx) => {
  surveyRefinement(survey as z.infer<typeof ZSurveyBase>, ctx);
});

export type TJsWorkspaceStateSurvey = z.infer<typeof ZJsWorkspaceStateSurvey>;

export const ZJsWorkspaceStateActionClass = ZActionClass.pick({
  id: true,
  key: true,
  type: true,
  name: true,
  noCodeConfig: true,
});

export type TJsWorkspaceStateActionClass = z.infer<typeof ZJsWorkspaceStateActionClass>;

export const ZJsWorkspaceStateWorkspaceSetting = ZWorkspace.pick({
  id: true,
  recontactDays: true,
  clickOutsideClose: true,
  overlay: true,
  placement: true,
  inAppSurveyBranding: true,
  styling: true,
});

export type TJsWorkspaceStateWorkspaceSetting = z.infer<typeof ZJsWorkspaceStateWorkspaceSetting>;

export const ZJsWorkspaceState = z.object({
  expiresAt: z.date(),
  data: z.object({
    surveys: z.array(ZJsWorkspaceStateSurvey),
    actionClasses: z.array(ZJsWorkspaceStateActionClass),
    workspace: ZJsWorkspaceStateWorkspaceSetting,
    recaptchaSiteKey: z.string().optional(),
  }),
});

export type TJsWorkspaceState = z.infer<typeof ZJsWorkspaceState>;

// A survey as the identify state names it for one user: hashString("<userId>:<surveyId>") in eight hex digits (js-core getSurveyKey, the server's sdkidentity.SurveyKey). The state never lists survey ids.
const ZJsSurveyKey = z.string().regex(/^[0-9a-f]{8}$/);

export const ZJsPersonState = z.object({
  expiresAt: z.date().nullable(),
  data: z.object({
    userId: z.string().nullable(),
    // An opaque contact token ("ct1." and base64url), never the contact's id; the display and response routes accept it as contactId.
    contactId: z.string().nullable(),
    segments: z.array(ZId), // segment ids the person belongs to
    displays: z.array(ZJsSurveyKey), // one key per display of an app survey, newest first
    responses: z.array(ZJsSurveyKey), // keys of the app surveys the person responded to
    lastDisplayAt: z.date().nullable(),
    language: z.string().optional(),
  }),
});

export type TJsPersonState = z.infer<typeof ZJsPersonState>;

export const ZJsFileUploadParams = z.object({
  file: z.object({ type: z.string(), name: z.string(), base64: z.string() }),
  params: ZUploadFileConfig,
});

export type TJsFileUploadParams = z.infer<typeof ZJsFileUploadParams>;
