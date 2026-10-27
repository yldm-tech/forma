import type { TFunction } from "i18next";
import { getDeclaredIngestedStorageKeys } from "@forma/types/embedded-data-resolver";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type { TSurvey } from "@forma/types/surveys/types";
import { getTextContent } from "@forma/types/surveys/validation";
import { recallToHeadline } from "../../../lib/survey/recall";
import { getElementsFromBlocks } from "../../survey-editor/lib/client-utils";

// The placeholders a step's templates (an HTTP body, a chat message, an attribute value, an email subject) can use, as the server's Template resolves them (apps/server/internal/workflow/template.go).

export interface TWorkflowTemplateVariable {
  /** The placeholder as written in a template, `{{answers.q1}}`. */
  token: string;
  label: string;
}

// The slot recall and headline resolution use when no language is selected.
const DEFAULT_LANGUAGE_CODE = "default";

export const toTemplateToken = (path: string): string => `{{${path}}}`;

// The elements whose answers are choice labels in the respondent's language, which also offer the language-independent choiceId and default placeholders.
const CHOICE_ELEMENT_TYPES = new Set<string>([
  TSurveyElementTypeEnum.MultipleChoiceSingle,
  TSurveyElementTypeEnum.MultipleChoiceMulti,
  TSurveyElementTypeEnum.Ranking,
]);

/** The variables offered next to a template: the response's and survey's own values, then each answer of the bound survey (its elements by headline, a choice element's choice id and default-language label after its answer, its hidden fields by id) and its variables. Without a bound survey only the fixed values are offered. */
export const buildWorkflowTemplateVariables = (
  survey: TSurvey | null,
  t: TFunction
): TWorkflowTemplateVariable[] => {
  const fixed: TWorkflowTemplateVariable[] = [
    { token: toTemplateToken("survey.name"), label: t("workspace.workflows.variable_survey_name") },
    { token: toTemplateToken("response.id"), label: t("workspace.workflows.variable_response_id") },
    { token: toTemplateToken("response.data"), label: t("workspace.workflows.variable_response_data") },
    { token: toTemplateToken("contact.id"), label: t("workspace.workflows.variable_contact_id") },
    {
      token: toTemplateToken("contact.attributes.email"),
      label: t("workspace.workflows.variable_contact_attribute", { key: "email" }),
    },
  ];
  if (!survey) return fixed;
  const elements = getElementsFromBlocks(survey.blocks).flatMap((element) => {
    const label =
      getTextContent(
        recallToHeadline(element.headline, survey, false, DEFAULT_LANGUAGE_CODE)[DEFAULT_LANGUAGE_CODE] ?? ""
      ) || element.id;
    const answer = { token: toTemplateToken(`answers.${element.id}`), label };
    if (!CHOICE_ELEMENT_TYPES.has(element.type)) return [answer];
    return [
      answer,
      {
        token: toTemplateToken(`answers.${element.id}.choiceId`),
        label: t("workspace.workflows.variable_choice_id", { label }),
      },
      {
        token: toTemplateToken(`answers.${element.id}.default`),
        label: t("workspace.workflows.variable_choice_default", { label }),
      },
    ];
  });
  const hiddenFields = getDeclaredIngestedStorageKeys(survey).map((fieldId) => ({
    token: toTemplateToken(`answers.${fieldId}`),
    label: fieldId,
  }));
  const variables = (survey.variables ?? []).map((variable) => ({
    token: toTemplateToken(`variables.${variable.id}`),
    label: variable.name,
  }));
  return [...fixed, ...elements, ...hiddenFields, ...variables];
};

/** value with text put where the selection of the control was, or at its end without one; returns the new value and the caret after the insertion. */
export const insertAtSelection = (
  value: string,
  text: string,
  selection: { start: number; end: number } | null
): { value: string; caret: number } => {
  const start = selection ? Math.min(selection.start, value.length) : value.length;
  const end = selection ? Math.min(Math.max(selection.end, start), value.length) : value.length;
  return { value: value.slice(0, start) + text + value.slice(end), caret: start + text.length };
};
