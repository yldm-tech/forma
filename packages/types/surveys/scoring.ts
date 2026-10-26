/**
 * Scoring: the points a survey's choices are worth and the total score the runtime keeps while a respondent answers. A single choice, multiple choice or picture selection element is scored once any of its choices has a score; the survey's total score is the number variable marked `isScore`, which the runtime raises by the score of every choice picked in a block when the block is submitted, before the block's logic runs, so logic, recall and quotas read it like any other variable. Kept free of zod so the survey runtime can import it without the schema library.
 */
import type { TResponseData, TResponseDataValue, TResponseVariables } from "../responses";
import { TSurveyElementTypeEnum } from "./constants";
import type {
  TSurveyElement,
  TSurveyMultipleChoiceElement,
  TSurveyPictureSelectionElement,
} from "./elements";
import type { TSurveyVariable } from "./types";

/** The refinement on a survey's variables, also reported by the Go document check (apps/server/internal/survey/surveydoc). */
export const SCORE_VARIABLE_UNIQUE_MESSAGE = "Only one variable can hold the total score";

/** The name the editor gives the total score variable it creates, with a numeric suffix when the survey already has a variable of that name. */
export const SCORE_VARIABLE_DEFAULT_NAME = "score";

export type TScorableElement = TSurveyMultipleChoiceElement | TSurveyPictureSelectionElement;

type TNumberVariable = Extract<TSurveyVariable, { type: "number" }>;

/** Whether an element's type can carry choice scores: single choice, multiple choice and picture selection. */
export const isScorableElement = (element: TSurveyElement): element is TScorableElement =>
  element.type === TSurveyElementTypeEnum.MultipleChoiceSingle ||
  element.type === TSurveyElementTypeEnum.MultipleChoiceMulti ||
  element.type === TSurveyElementTypeEnum.PictureSelection;

/** Whether an element is scored: a scorable element with at least one choice that has a score. */
export const isScoredElement = (element: TSurveyElement): element is TScorableElement =>
  isScorableElement(element) && element.choices.some((choice) => typeof choice.score === "number");

/** The survey's total score variable: the first number variable marked `isScore`, undefined when there is none. */
export const getScoreVariable = (
  variables: readonly TSurveyVariable[] | null | undefined
): TNumberVariable | undefined =>
  variables?.find(
    (variable): variable is TNumberVariable => variable.type === "number" && variable.isScore === true
  );

/** A name for a new total score variable that no variable of the survey has yet. */
export const getNewScoreVariableName = (variables: readonly TSurveyVariable[]): string => {
  const taken = new Set(variables.map((variable) => variable.name));
  if (!taken.has(SCORE_VARIABLE_DEFAULT_NAME)) return SCORE_VARIABLE_DEFAULT_NAME;
  let suffix = 2;
  while (taken.has(`${SCORE_VARIABLE_DEFAULT_NAME}_${String(suffix)}`)) suffix++;
  return `${SCORE_VARIABLE_DEFAULT_NAME}_${String(suffix)}`;
};

/** The bound on a choice's score, either way. */
export const CHOICE_SCORE_LIMIT = 1_000_000;

/** A total rounded to six decimals, so scores of 0.1 and 0.2 add up to 0.3 rather than 0.30000000000000004. */
const roundScore = (n: number): number => Math.round(n * 1e6) / 1e6;

const choiceScore = (choice: { score?: number } | undefined): number =>
  typeof choice?.score === "number" && Number.isFinite(choice.score) ? choice.score : 0;

/** The choice of a single or multiple choice element an answer names: the choice whose label, in any of the survey's languages, is the answer, and otherwise the "other" choice, whose answer is the respondent's own text. */
const findAnsweredChoice = (element: TSurveyMultipleChoiceElement, answer: string) => {
  const labelled = element.choices.find(
    (choice) => choice.id !== "other" && Object.values(choice.label).includes(answer)
  );
  return labelled ?? element.choices.find((choice) => choice.id === "other");
};

/**
 * The points an answer to a scored element earns: the sum of the scores of the choices it picked, each choice counted once, a choice without a score counting 0. Undefined when the element is not scored or has no answer, so an unscored or skipped element leaves the total alone.
 *
 * Single and multiple choice answers are labels (the "other" choice's answer is free text); picture selection answers are choice ids.
 */
export const getElementScore = (element: TSurveyElement, answer: TResponseDataValue): number | undefined => {
  if (!isScoredElement(element)) return undefined;
  const values = (Array.isArray(answer) ? answer : [answer]).filter(
    (value): value is string => typeof value === "string" && value !== ""
  );
  if (values.length === 0) return undefined;

  const picked = new Map<string, number>();
  for (const value of values) {
    const choice =
      element.type === TSurveyElementTypeEnum.PictureSelection
        ? element.choices.find((c) => c.id === value)
        : findAnsweredChoice(element, value);
    if (choice) picked.set(choice.id, choiceScore(choice));
  }
  let total = 0;
  for (const score of picked.values()) total += score;
  return total;
};

/**
 * The variables after a block is submitted: the total score variable raised by the points the block's answers earned. The variables are returned as they are when the survey has no total score variable or no element of the block earned points (none is scored, or none was answered).
 */
export const applyBlockScore = (
  variables: readonly TSurveyVariable[] | null | undefined,
  elements: readonly TSurveyElement[],
  data: TResponseData,
  current: TResponseVariables
): TResponseVariables => {
  const scoreVariable = getScoreVariable(variables);
  if (!scoreVariable) return current;
  let earned: number | undefined;
  for (const element of elements) {
    const score = getElementScore(element, data[element.id]);
    if (score !== undefined) earned = (earned ?? 0) + score;
  }
  if (earned === undefined) return current;
  const before = current[scoreVariable.id];
  const base = typeof before === "number" && Number.isFinite(before) ? before : scoreVariable.value;
  return { ...current, [scoreVariable.id]: roundScore(base + earned) };
};
