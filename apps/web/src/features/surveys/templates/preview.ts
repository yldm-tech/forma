import type { TSurvey } from "@forma/types/surveys/types";
import type { SurveyTemplate } from "../api";
import type { OrganizationTemplate, OrganizationTemplatePreset } from "./organization-templates";

/** A catalog template's preset as the server sends it (the Go `survey.Preset`): the survey document the template creates, rendered in the language it was asked in with the workspace's name filled in. */
export type TemplatePreset = Partial<
  Pick<TSurvey, "name" | "welcomeCard" | "blocks" | "endings" | "hiddenFields" | "variables" | "metadata">
>;

/**
 * forma2's `{ ...getMinimalSurvey(t), ...template.preset }`: the template as a survey the respondent runtime can render in preview mode, of the type the gallery creates. Everything the preset leaves out takes the value a new survey would have, so the preview behaves like the draft the template creates; nothing here is ever saved.
 */
export const templatePreviewSurvey = (template: SurveyTemplate, type: "link" | "app"): TSurvey => {
  const preset = template.preset as TemplatePreset;
  const now = new Date();
  return {
    // The template's id, so the preview starts over when the gallery switches to another template.
    id: template.id,
    createdAt: now,
    updatedAt: now,
    name: preset.name ?? template.name,
    type,
    workspaceId: "",
    createdBy: null,
    status: "inProgress",
    displayOption: "displayOnce",
    autoClose: null,
    triggers: [],
    recontactDays: null,
    displayLimit: null,
    welcomeCard: preset.welcomeCard ?? { enabled: false, timeToFinish: false, showResponseCount: false },
    questions: [],
    blocks: preset.blocks ?? [],
    endings: preset.endings ?? [],
    hiddenFields: preset.hiddenFields ?? { enabled: true, fieldIds: [] },
    variables: preset.variables ?? [],
    followUps: [],
    delay: 0,
    publishOn: null,
    closeOn: null,
    autoComplete: null,
    workspaceOverwrites: null,
    styling: null,
    showLanguageSwitch: false,
    surveyClosedMessage: null,
    segment: null,
    singleUse: null,
    isVerifyEmailEnabled: false,
    recaptcha: null,
    isBackButtonHidden: false,
    isAutoProgressingEnabled: false,
    isCaptureIpEnabled: false,
    isAnonymizeResponsesEnabled: false,
    isProgressSavingEnabled: false,
    pin: null,
    displayPercentage: null,
    languages: [],
    metadata: preset.metadata ?? {},
    slug: null,
  };
};

/** An organization's template as a survey the respondent runtime can render in preview mode: like a catalog template, with the styling and closed message it was saved with. */
export const organizationTemplatePreviewSurvey = (
  template: Pick<OrganizationTemplate, "id" | "name" | "description"> & {
    preset: OrganizationTemplatePreset;
  },
  type: "link" | "app"
): TSurvey => ({
  ...templatePreviewSurvey(
    { id: template.id, name: template.name, description: template.description, preset: template.preset },
    type
  ),
  styling: template.preset.styling ?? null,
  surveyClosedMessage: template.preset.surveyClosedMessage ?? null,
});
