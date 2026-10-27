import {
  type TWorkflowDefinition,
  type TWorkflowSetContactAttributeActionNode,
  WORKFLOW_ACTIONS,
} from "../definition";

// SDK-protected contact attributes (the workspace's App connection settings) are ones the SDK may not write, because targeting trusts them, such as a plan. A workflow may still write them: the warning and the confirmations here make sure that is on purpose when the value comes from what a respondent entered. The server does not refuse it (decision D4).

// Every placeholder of a template, {{path}} with optional spaces inside the braces, as the server's template matches them (apps/server/internal/workflow/template.go).
const PLACEHOLDER = /\{\{\s*([A-Za-z0-9_.:-]+)\s*\}\}/g;

const ANSWERS_PREFIX = "answers.";
const CHOICE_ID_SUFFIX = ".choiceId";
const CONTACT_ATTRIBUTES_PREFIX = "contact.attributes.";

/** Whether the placeholder's value is one the respondent controls: all answers (response.data), an answer or hidden field, a variable computed from answers, or a contact attribute the SDK may write (one not protected). A choice's id is not: it is always one of the ids the author gave the choices (an "Other" text resolves to the other choice's id), so the respondent only picks among the author's values. */
const isRespondentSourcedPath = (path: string, protectedKeys: readonly string[]): boolean => {
  if (path === "response.data") return true;
  if (path.startsWith(ANSWERS_PREFIX)) {
    const key = path.slice(ANSWERS_PREFIX.length);
    // Element and hidden field ids are [a-zA-Z0-9_-], so a key ending in .choiceId is always a choice id placeholder.
    if (key.endsWith(CHOICE_ID_SUFFIX) && key.length > CHOICE_ID_SUFFIX.length) return false;
    return key !== "";
  }
  if (path.startsWith("variables.")) return path.length > "variables.".length;
  if (path.startsWith(CONTACT_ATTRIBUTES_PREFIX)) {
    const key = path.slice(CONTACT_ATTRIBUTES_PREFIX.length);
    return key !== "" && !protectedKeys.includes(key);
  }
  return false;
};

/** Whether a set_contact_attribute value is filled from something the respondent controls: their answers, hidden fields or variables, or a contact attribute the SDK may write (protectedKeys are the ones it may not). */
export const isRespondentSourcedValue = (value: string, protectedKeys: readonly string[]): boolean =>
  Array.from(value.matchAll(PLACEHOLDER)).some((match) =>
    isRespondentSourcedPath(match[1] ?? "", protectedKeys)
  );

/** Whether the step writes an SDK-protected attribute from what the respondent controls, the write that needs a confirmation. */
export const writesProtectedAttributeFromResponse = (
  config: TWorkflowSetContactAttributeActionNode["config"],
  protectedKeys: readonly string[]
): boolean =>
  protectedKeys.includes(config.attributeKey) && isRespondentSourcedValue(config.value, protectedKeys);

/** Whether changing the step from config to next makes it a protected write the author has not confirmed yet: it becomes one, or it already was one and now writes a different protected attribute, which the earlier confirmation did not name. */
export const needsProtectedWriteConfirmation = (
  config: TWorkflowSetContactAttributeActionNode["config"],
  next: TWorkflowSetContactAttributeActionNode["config"],
  protectedKeys: readonly string[]
): boolean =>
  writesProtectedAttributeFromResponse(next, protectedKeys) &&
  (!writesProtectedAttributeFromResponse(config, protectedKeys) || next.attributeKey !== config.attributeKey);

/** The steps of the definition that write an SDK-protected attribute from what the respondent controls, in definition order. */
export const getProtectedAttributeWrites = (
  definition: TWorkflowDefinition | null,
  protectedKeys: readonly string[]
): TWorkflowSetContactAttributeActionNode[] => {
  if (!definition || protectedKeys.length === 0) return [];
  return definition.nodes.filter(
    (node): node is TWorkflowSetContactAttributeActionNode =>
      node.type === "action" &&
      node.actionType === WORKFLOW_ACTIONS.SET_CONTACT_ATTRIBUTE &&
      writesProtectedAttributeFromResponse(node.config, protectedKeys)
  );
};
