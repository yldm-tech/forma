import type { TSurveyBlockLogic } from "@forma/types/surveys/blocks";
import {
  type TConditionGroup,
  type TSingleCondition,
  operatorsWithoutRightOperand,
} from "@forma/types/surveys/logic";
import type { TSurvey } from "@forma/types/surveys/types";
import { getTextContent, isConditionGroup } from "@forma/types/surveys/validation";

/**
 * One rule a survey breaks: a zod issue from the editor's own check, or one of the issues the server returns with a 400 validation error (`details.issues`, the same path/message/params shape).
 */
export interface SurveyIssue {
  path: readonly PropertyKey[];
  message: string;
  params?: Record<string, unknown>;
}

/** The cards an issue list marks invalid, the DOM id of the first one to scroll to, and the block to scroll to instead when that target sits in a block and is not rendered (a collapsed block unmounts its content). */
export interface InvalidCards {
  invalidIds: string[];
  firstInvalidScrollId: string | null;
  fallbackScrollId: string | null;
}

/**
 * forma2's mapping from validation issues to the editor's cards: an element issue flags the element, a block-name issue the block, a logic issue the rule (or, without a rule index, every rule of the block), a welcome card issue "start" and an ending issue the ending. A rule issue scrolls to the rule; a block-wide logic issue to the block, whose card carries the red bar.
 */
export const getInvalidCards = (survey: TSurvey, issues: readonly SurveyIssue[]): InvalidCards => {
  const invalidIds: string[] = [];
  let firstInvalidScrollId: string | null = null;
  let fallbackScrollId: string | null = null;
  const flag = (id: string | undefined) => {
    if (id && !invalidIds.includes(id)) invalidIds.push(id);
  };

  for (const { path } of issues) {
    const [section, index, field, subIndex] = path;
    if (section === "blocks") {
      const block = typeof index === "number" ? survey.blocks[index] : undefined;
      if (field === "elements" && typeof subIndex === "number") {
        const element = block?.elements[subIndex];
        flag(element?.id);
        if (firstInvalidScrollId === null && element) {
          firstInvalidScrollId = element.id;
          fallbackScrollId = block?.id ?? null;
        }
      } else if (field === "name") {
        flag(block?.id);
        firstInvalidScrollId ??= block?.id ?? null;
      } else if (field === "logic" && typeof subIndex === "number") {
        const ruleId = block?.logic?.[subIndex]?.id;
        flag(ruleId);
        // The rule itself, not its block: a block with several questions puts its rules well below its top.
        if (firstInvalidScrollId === null && ruleId) {
          firstInvalidScrollId = logicRuleDomId(ruleId);
          fallbackScrollId = block?.id ?? null;
        } else {
          firstInvalidScrollId ??= block?.id ?? null;
        }
      } else if (field === "logic") {
        for (const logicItem of block?.logic ?? []) flag(logicItem.id);
        firstInvalidScrollId ??= block?.id ?? null;
      }
    } else if (section === "welcomeCard") {
      flag("start");
      firstInvalidScrollId ??= "start";
    } else if (section === "endings") {
      const endingId = typeof index === "number" ? survey.endings[index]?.id : undefined;
      flag(endingId);
      firstInvalidScrollId ??= endingId ?? null;
    }
  }

  return { invalidIds, firstInvalidScrollId, fallbackScrollId };
};

/** The DOM id of a logic rule's card, which a rule issue scrolls to. */
export const logicRuleDomId = (ruleId: string): string => `logic-rule-${ruleId}`;

/** What the editor reports for the first issue: a message, and whether it belongs to the Language tab. */
export interface IssueReport {
  message: string;
  showLanguageTab: boolean;
}

/** The translated messages the editor reports issues with; numbers are 1-based, as the author sees them. */
export interface IssueMessages {
  surveyNameRequired: () => string;
  blockNameRequired: (blockNumber: number) => string;
  questionTextMissing: (blockNumber: number, questionNumber: number) => string;
  optionLabelMissing: (blockNumber: number, questionNumber: number, optionNumber: number) => string;
  logicRuleIncomplete: (blockNumber: number, ruleNumber: number) => string;
  languageLabel: (code: string) => string;
}

const isBlank = (text: string | undefined): boolean => getTextContent(text ?? "").trim() === "";

const isUnset = (value: unknown): boolean =>
  value === undefined || value === null || value === "" || (Array.isArray(value) && value.length === 0);

const hasUnsetOperand = (condition: TSingleCondition | TConditionGroup): boolean => {
  if (isConditionGroup(condition)) return condition.conditions.some(hasUnsetOperand);
  if (isUnset(condition.leftOperand?.value)) return true;
  if ((operatorsWithoutRightOperand as readonly string[]).includes(condition.operator)) return false;
  return isUnset(condition.rightOperand?.value);
};

/** Whether a logic rule still has a blank to fill in: a condition without its question or value, or an action without its target, variable or value. Only such a rule is called incomplete; a complete rule can still be refused (a later block, a choice since deleted, an NPS value out of range), and then the issue's own message names the reason. */
export const isLogicRuleIncomplete = (rule: TSurveyBlockLogic): boolean =>
  hasUnsetOperand(rule.conditions) ||
  rule.actions.some((action) =>
    action.objective === "calculate"
      ? isUnset(action.variableId) || isUnset(action.value?.value)
      : isUnset(action.target)
  );

/**
 * The editor's own wording for the issues an author meets most, keyed by the issue's path. The messages in packages/types and the Go server are English and name internals ("logic no: 1 of block 2"); the v1/v3 APIs and their tests depend on them, so they stay as they are and the editor translates here. A missing-text rule is only reworded when the default text really is empty: the same path also carries "not present for the following languages", which keeps its own message. Anything without a mapping keeps the raw message.
 */
const mappedMessage = (
  survey: TSurvey,
  path: readonly PropertyKey[],
  messages: IssueMessages
): string | null => {
  const [section, blockIndex, field, index, subField, subIndex] = path;
  if (section === "name" && path.length === 1) return messages.surveyNameRequired();
  if (section !== "blocks" || typeof blockIndex !== "number") return null;
  const block = survey.blocks[blockIndex];
  if (field === "name") return messages.blockNameRequired(blockIndex + 1);
  if (field === "logic" && typeof index === "number") {
    const rule = block?.logic?.[index];
    return rule && isLogicRuleIncomplete(rule)
      ? messages.logicRuleIncomplete(blockIndex + 1, index + 1)
      : null;
  }
  if (field !== "elements" || typeof index !== "number") return null;
  const element = block?.elements[index];
  if (!element) return null;
  if (subField === "headline" && path.length === 5 && isBlank(element.headline.default)) {
    return messages.questionTextMissing(blockIndex + 1, index + 1);
  }
  if (subField === "choices" && typeof subIndex === "number" && path.length === 6 && "choices" in element) {
    const choice = element.choices[subIndex];
    if (choice && "label" in choice && isBlank(choice.label.default)) {
      return messages.optionLabelMissing(blockIndex + 1, index + 1, subIndex + 1);
    }
  }
  return null;
};

/**
 * The toast for the first issue. A translation issue lists the languages it concerns by name, replacing the codes after the "-fLang-" marker, and sends the author to the Language tab; the common issues get the editor's translated wording; anything else is shown as the schema or server worded it.
 */
export const describeFirstIssue = (
  survey: TSurvey,
  issue: SurveyIssue,
  messages: IssueMessages
): IssueReport => {
  const codes = issue.params?.invalidLanguageCodes;
  if (Array.isArray(codes) && codes.length > 0) {
    const labels = codes.map((code) =>
      typeof code === "string" ? messages.languageLabel(code) : String(code)
    );
    return {
      message: `${issue.message.split("-fLang-")[0] ?? ""} ${labels.join(", ")}`,
      showLanguageTab: true,
    };
  }
  return { message: mappedMessage(survey, issue.path, messages) ?? issue.message, showLanguageTab: false };
};

/** The issues of a server validation error (`details.issues`), or none for any other error body. */
export const issuesFromDetails = (details: Record<string, unknown> | undefined): SurveyIssue[] => {
  const issues = details?.issues;
  if (!Array.isArray(issues)) return [];
  return issues.filter(
    (issue): issue is SurveyIssue =>
      typeof issue === "object" &&
      issue !== null &&
      Array.isArray((issue as SurveyIssue).path) &&
      typeof (issue as SurveyIssue).message === "string"
  );
};
