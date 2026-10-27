import {
  type TWorkflowDefinition,
  type TWorkflowSetContactAttributeActionNode,
  WORKFLOW_ACTIONS,
} from "../definition";

// SDK-protected contact attributes (the workspace's App connection settings) are ones the SDK may not write, because targeting trusts them, such as a plan. A workflow may still write them: the warning and the confirmations here make sure that is on purpose when the value comes from what a respondent entered. The server does not refuse it (decision D4).

// A placeholder whose value the respondent controls: an answer (elements and hidden fields alike) or a variable computed from answers. Matched per placeholder, as the server's template does (apps/server/internal/workflow/template.go).
const RESPONDENT_PLACEHOLDER = /\{\{\s*(?:answers|variables)\.[A-Za-z0-9_.:-]+\s*\}\}/;

/** Whether a set_contact_attribute value is filled from the respondent's answers, hidden fields or variables. */
export const isRespondentSourcedValue = (value: string): boolean => RESPONDENT_PLACEHOLDER.test(value);

/** Whether the step writes an SDK-protected attribute from what the respondent entered, the write that needs a confirmation. */
export const writesProtectedAttributeFromResponse = (
  config: TWorkflowSetContactAttributeActionNode["config"],
  protectedKeys: readonly string[]
): boolean => protectedKeys.includes(config.attributeKey) && isRespondentSourcedValue(config.value);

/** The steps of the definition that write an SDK-protected attribute from what the respondent entered, in definition order. */
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
