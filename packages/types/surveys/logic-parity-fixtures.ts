import { deriveLegacyEmbeddedData } from "../embedded-data-resolver";
import { type TJsWorkspaceStateSurvey } from "../js";
import { type TResponseData, type TResponseVariables } from "../responses";
import { TSurveyElementTypeEnum } from "./constants";
import {
  type TConditionGroup,
  type TSingleCondition,
  type TSurveyLogicConditionsOperator,
  ZSurveyLogicConditionsOperator,
} from "./logic";
import { type TSurveyVariable } from "./types";

/**
 * The parity corpus for the two survey-logic engines: `packages/surveys/src/lib/logic.ts` (the
 * renderer, which decides what the respondent is shown) and `apps/web/lib/surveyLogic/utils.ts` (the
 * server, which screens the submitted response against quotas and follow-up conditions). The two are
 * near-copies, and a disagreement between them is its own bug class — a respondent routed one way in
 * the browser and counted the other way on the server, with no error anywhere.
 *
 * The compile-time `satisfies never` gate at the bottom of each `switch` already stops an operator
 * from existing on one side only. Nothing checked that the two arms *behave* the same, which is why
 * this table exists: one case list, two suites (`packages/surveys/src/lib/logic.parity.test.ts` and
 * `apps/web/lib/surveyLogic/utils.parity.test.ts`) that each drive their own engine through it and
 * assert the same expected value.
 *
 * It lives in `@forma/types` because that is the only package both engines already depend on —
 * `apps/web` cannot import the renderer's evaluator (it is internal to `@forma/surveys`, which
 * publishes only its bundle entry points) and `packages/surveys` cannot import anything from the app.
 * Fixtures only: no evaluator, no assertion, nothing either engine loads at runtime.
 */
const PARITY_VARIABLES: TSurveyVariable[] = [
  { id: "var1", name: "Variable 1", type: "text", value: "string value" },
  { id: "var2", name: "Variable 2", type: "number", value: 50 },
];

const PARITY_HIDDEN_FIELD_IDS = ["hfText", "hfNumber", "hfList", "hfObject", "hfStatus", "hfEmpty"] as const;

export const PARITY_LANGUAGE = "default";

export const PARITY_SURVEY: TJsWorkspaceStateSurvey = {
  id: "survey1",
  name: "Logic parity survey",
  questions: [],
  blocks: [
    {
      id: "block1",
      name: "Block 1",
      elements: [
        {
          id: "q1",
          type: TSurveyElementTypeEnum.OpenText,
          headline: { default: "Question 1" },
          required: true,
          inputType: "text",
          charLimit: { enabled: false },
        },
        {
          id: "q2",
          type: TSurveyElementTypeEnum.OpenText,
          headline: { default: "Question 2" },
          required: true,
          inputType: "number",
          charLimit: { enabled: false },
        },
        {
          id: "q3",
          type: TSurveyElementTypeEnum.MultipleChoiceSingle,
          headline: { default: "Question 3" },
          required: true,
          choices: [
            { id: "opt1", label: { default: "Option 1" } },
            { id: "opt2", label: { default: "Option 2" } },
            { id: "other", label: { default: "Other" } },
          ],
        },
        {
          id: "q4",
          type: TSurveyElementTypeEnum.MultipleChoiceMulti,
          headline: { default: "Question 4" },
          required: true,
          choices: [
            { id: "opt1", label: { default: "Option 1" } },
            { id: "opt2", label: { default: "Option 2" } },
            { id: "opt3", label: { default: "Option 3" } },
          ],
        },
        {
          id: "q5",
          type: TSurveyElementTypeEnum.Date,
          headline: { default: "Question 5" },
          required: true,
          format: "d-M-y",
        },
        {
          id: "q6",
          type: TSurveyElementTypeEnum.FileUpload,
          headline: { default: "Question 6" },
          required: true,
          allowMultipleFiles: false,
        },
        {
          id: "q7",
          type: TSurveyElementTypeEnum.PictureSelection,
          headline: { default: "Question 7" },
          required: true,
          allowMulti: true,
          choices: [
            { id: "pic1", imageUrl: "url1" },
            { id: "pic2", imageUrl: "url2" },
          ],
        },
        {
          id: "q8",
          type: TSurveyElementTypeEnum.Matrix,
          headline: { default: "Question 8" },
          required: true,
          rows: [
            { id: "row1", label: { default: "Row 1" } },
            { id: "row2", label: { default: "Row 2" } },
          ],
          columns: [
            { id: "col1", label: { default: "Column 1" } },
            { id: "col2", label: { default: "Column 2" } },
          ],
          shuffleOption: "none",
        },
        {
          // "Other" is NOT last here, and that is the point: the editor orders special choices
          // `[…regular, other, none]`, so every element that offers both an "Other" box and a "None
          // of the above" choice has this shape.
          id: "q9",
          type: TSurveyElementTypeEnum.MultipleChoiceSingle,
          headline: { default: "Question 9" },
          required: true,
          choices: [
            { id: "opt1", label: { default: "Option 1" } },
            { id: "other", label: { default: "Other" } },
            { id: "none", label: { default: "None of the above" } },
          ],
        },
      ],
    },
  ],
  variables: PARITY_VARIABLES,
  hiddenFields: { enabled: true, fieldIds: [...PARITY_HIDDEN_FIELD_IDS] },
  embeddedFields: deriveLegacyEmbeddedData({
    variables: PARITY_VARIABLES,
    hiddenFields: { enabled: true, fieldIds: [...PARITY_HIDDEN_FIELD_IDS] },
  }),
  autoClose: null,
  type: "link",
  delay: 0,
  displayLimit: 0,
  displayOption: "displayMultiple",
  displayPercentage: 0,
  recaptcha: { enabled: false, threshold: 0.5 },
  isBackButtonHidden: false,
  isAutoProgressingEnabled: false,
  segment: null,
  welcomeCard: { enabled: true, showResponseCount: true, timeToFinish: true },
  triggers: [],
  styling: null,
  status: "inProgress",
  showLanguageSwitch: false,
  languages: [],
  endings: [],
  workspaceOverwrites: null,
  recontactDays: null,
};

export const PARITY_RESPONSE_DATA: TResponseData = {
  q1: "test answer",
  q2: 42,
  q3: "Option 1",
  q4: ["Option 1", "Option 2"],
  q5: "2023-01-01",
  q6: "https://example.com/upload.pdf",
  q7: ["pic1", "pic2"],
  q8: { "Row 1": "Column 1", "Row 2": "Column 2" },
  q9: "Option 1",
  hfText: "hidden value",
  hfNumber: "123",
  hfList: ["a", "b"],
  hfObject: { first: "Jane", last: "Doe" },
  hfStatus: "clicked",
  hfEmpty: "",
};

export const PARITY_VARIABLE_DATA: TResponseVariables = {
  var1: "string value",
  var2: 123,
};

export interface TLogicParityCase {
  /** Reads as `<operator>: <what this case pins>` in the test name. */
  readonly name: string;
  readonly condition: TSingleCondition;
  /** Merged over {@link PARITY_RESPONSE_DATA}. */
  readonly data?: TResponseData;
  /** Merged over {@link PARITY_VARIABLE_DATA}. */
  readonly variables?: TResponseVariables;
  readonly embeddedValues?: TResponseData;
  readonly expected: boolean;
}

export const LOGIC_PARITY_CASES: readonly TLogicParityCase[] = [
  // equals
  {
    name: "equals: hidden field matches a static string",
    condition: {
      id: "c-equals-1",
      operator: "equals",
      leftOperand: { type: "hiddenField", value: "hfText" },
      rightOperand: { type: "static", value: "hidden value" },
    },
    expected: true,
  },
  {
    name: "equals: hidden field differs from a static string",
    condition: {
      id: "c-equals-2",
      operator: "equals",
      leftOperand: { type: "hiddenField", value: "hfText" },
      rightOperand: { type: "static", value: "other value" },
    },
    expected: false,
  },
  {
    name: "equals: a date element compares by instant, not by string",
    condition: {
      id: "c-equals-3",
      operator: "equals",
      leftOperand: { type: "element", value: "q5" },
      rightOperand: { type: "static", value: "2023-01-01" },
    },
    expected: true,
  },
  {
    name: "equals: a single-select answer resolves to its choice id",
    condition: {
      id: "c-equals-4",
      operator: "equals",
      leftOperand: { type: "element", value: "q3" },
      rightOperand: { type: "static", value: "opt1" },
    },
    expected: true,
  },
  {
    name: "equals: a one-element right operand array contains the left string",
    condition: {
      id: "c-equals-5",
      operator: "equals",
      leftOperand: { type: "hiddenField", value: "hfText" },
      rightOperand: { type: "element", value: "q4" },
    },
    data: { hfText: "Option 1", q4: ["Option 1"] },
    expected: true,
  },
  {
    name: "equals: a number variable compares against a hidden field numerically",
    condition: {
      id: "c-equals-6",
      operator: "equals",
      leftOperand: { type: "variable", value: "var2" },
      rightOperand: { type: "hiddenField", value: "hfNumber" },
    },
    expected: true,
  },
  {
    name: "equals: a matrix row resolves to its selected column index",
    condition: {
      id: "c-equals-7",
      operator: "equals",
      leftOperand: { type: "element", value: "q8", meta: { row: "0" } },
      rightOperand: { type: "static", value: "0" },
    },
    expected: true,
  },
  {
    name: "equals: a free-text answer resolves to `other` when the element offers an Other box",
    condition: {
      id: "c-equals-8",
      operator: "equals",
      leftOperand: { type: "element", value: "q3" },
      rightOperand: { type: "static", value: "other" },
    },
    data: { q3: "something the respondent typed" },
    expected: true,
  },
  {
    name: "equals: `other` still resolves when a None-of-the-above choice sits after it",
    condition: {
      id: "c-equals-9",
      operator: "equals",
      leftOperand: { type: "element", value: "q9" },
      rightOperand: { type: "static", value: "other" },
    },
    data: { q9: "something the respondent typed" },
    expected: true,
  },

  // doesNotEqual
  {
    name: "doesNotEqual: hidden field differs from a static string",
    condition: {
      id: "c-ne-1",
      operator: "doesNotEqual",
      leftOperand: { type: "hiddenField", value: "hfText" },
      rightOperand: { type: "static", value: "other value" },
    },
    expected: true,
  },
  {
    name: "doesNotEqual: hidden field matches a static string",
    condition: {
      id: "c-ne-2",
      operator: "doesNotEqual",
      leftOperand: { type: "hiddenField", value: "hfText" },
      rightOperand: { type: "static", value: "hidden value" },
    },
    expected: false,
  },
  {
    name: "doesNotEqual: a picture selection containing the option is not unequal to it",
    condition: {
      id: "c-ne-3",
      operator: "doesNotEqual",
      leftOperand: { type: "element", value: "q7" },
      rightOperand: { type: "static", value: "pic1" },
    },
    expected: false,
  },
  {
    name: "doesNotEqual: a single selection stored as a one-element array is not unequal to it",
    condition: {
      id: "c-ne-4",
      operator: "doesNotEqual",
      leftOperand: { type: "element", value: "q4" },
      rightOperand: { type: "static", value: "opt1" },
    },
    data: { q4: ["Option 1"] },
    expected: false,
  },

  // contains / doesNotContain
  {
    name: "contains: substring present",
    condition: {
      id: "c-contains-1",
      operator: "contains",
      leftOperand: { type: "hiddenField", value: "hfText" },
      rightOperand: { type: "static", value: "hidden" },
    },
    expected: true,
  },
  {
    name: "contains: substring absent",
    condition: {
      id: "c-contains-2",
      operator: "contains",
      leftOperand: { type: "hiddenField", value: "hfText" },
      rightOperand: { type: "static", value: "visible" },
    },
    expected: false,
  },
  {
    name: "contains: an unset operand is not the literal text `undefined`",
    condition: {
      id: "c-contains-3",
      operator: "contains",
      leftOperand: { type: "hiddenField", value: "hfMissing" },
      rightOperand: { type: "static", value: "defin" },
    },
    expected: false,
  },
  {
    name: "doesNotContain: substring absent",
    condition: {
      id: "c-notcontains-1",
      operator: "doesNotContain",
      leftOperand: { type: "hiddenField", value: "hfText" },
      rightOperand: { type: "static", value: "visible" },
    },
    expected: true,
  },
  {
    name: "doesNotContain: substring present",
    condition: {
      id: "c-notcontains-2",
      operator: "doesNotContain",
      leftOperand: { type: "hiddenField", value: "hfText" },
      rightOperand: { type: "static", value: "hidden" },
    },
    expected: false,
  },
  {
    name: "doesNotContain: an unset operand contains nothing",
    condition: {
      id: "c-notcontains-3",
      operator: "doesNotContain",
      leftOperand: { type: "hiddenField", value: "hfMissing" },
      rightOperand: { type: "static", value: "defin" },
    },
    expected: true,
  },

  // startsWith / doesNotStartWith
  {
    name: "startsWith: prefix matches",
    condition: {
      id: "c-starts-1",
      operator: "startsWith",
      leftOperand: { type: "hiddenField", value: "hfText" },
      rightOperand: { type: "static", value: "hidden" },
    },
    expected: true,
  },
  {
    name: "startsWith: prefix does not match",
    condition: {
      id: "c-starts-2",
      operator: "startsWith",
      leftOperand: { type: "hiddenField", value: "hfText" },
      rightOperand: { type: "static", value: "value" },
    },
    expected: false,
  },
  {
    name: "doesNotStartWith: prefix does not match",
    condition: {
      id: "c-notstarts-1",
      operator: "doesNotStartWith",
      leftOperand: { type: "hiddenField", value: "hfText" },
      rightOperand: { type: "static", value: "value" },
    },
    expected: true,
  },
  {
    name: "doesNotStartWith: prefix matches",
    condition: {
      id: "c-notstarts-2",
      operator: "doesNotStartWith",
      leftOperand: { type: "hiddenField", value: "hfText" },
      rightOperand: { type: "static", value: "hidden" },
    },
    expected: false,
  },

  // endsWith / doesNotEndWith
  {
    name: "endsWith: suffix matches",
    condition: {
      id: "c-ends-1",
      operator: "endsWith",
      leftOperand: { type: "hiddenField", value: "hfText" },
      rightOperand: { type: "static", value: "value" },
    },
    expected: true,
  },
  {
    name: "endsWith: suffix does not match",
    condition: {
      id: "c-ends-2",
      operator: "endsWith",
      leftOperand: { type: "hiddenField", value: "hfText" },
      rightOperand: { type: "static", value: "hidden" },
    },
    expected: false,
  },
  {
    name: "doesNotEndWith: suffix does not match",
    condition: {
      id: "c-notends-1",
      operator: "doesNotEndWith",
      leftOperand: { type: "hiddenField", value: "hfText" },
      rightOperand: { type: "static", value: "hidden" },
    },
    expected: true,
  },
  {
    name: "doesNotEndWith: suffix matches",
    condition: {
      id: "c-notends-2",
      operator: "doesNotEndWith",
      leftOperand: { type: "hiddenField", value: "hfText" },
      rightOperand: { type: "static", value: "value" },
    },
    expected: false,
  },

  // isSubmitted / isSkipped
  {
    name: "isSubmitted: an answered open text",
    condition: {
      id: "c-submitted-1",
      operator: "isSubmitted",
      leftOperand: { type: "element", value: "q1" },
    },
    expected: true,
  },
  {
    name: "isSubmitted: a file upload the respondent skipped",
    condition: {
      id: "c-submitted-2",
      operator: "isSubmitted",
      leftOperand: { type: "element", value: "q6" },
    },
    data: { q6: "skipped" },
    expected: false,
  },
  {
    name: "isSubmitted: a non-empty list answer",
    condition: {
      id: "c-submitted-3",
      operator: "isSubmitted",
      leftOperand: { type: "hiddenField", value: "hfList" },
    },
    expected: true,
  },
  {
    name: "isSkipped: an unset operand",
    condition: {
      id: "c-skipped-1",
      operator: "isSkipped",
      leftOperand: { type: "hiddenField", value: "hfMissing" },
    },
    expected: true,
  },
  {
    name: "isSkipped: an answered operand",
    condition: {
      id: "c-skipped-2",
      operator: "isSkipped",
      leftOperand: { type: "hiddenField", value: "hfText" },
    },
    expected: false,
  },
  {
    name: "isSkipped: a cleared answer",
    condition: {
      id: "c-skipped-3",
      operator: "isSkipped",
      leftOperand: { type: "hiddenField", value: "hfEmpty" },
    },
    expected: true,
  },

  // numeric comparisons
  {
    name: "isGreaterThan: above the threshold",
    condition: {
      id: "c-gt-1",
      operator: "isGreaterThan",
      leftOperand: { type: "element", value: "q2" },
      rightOperand: { type: "static", value: 10 },
    },
    expected: true,
  },
  {
    name: "isGreaterThan: below the threshold",
    condition: {
      id: "c-gt-2",
      operator: "isGreaterThan",
      leftOperand: { type: "element", value: "q2" },
      rightOperand: { type: "static", value: 100 },
    },
    expected: false,
  },
  {
    name: "isLessThan: below the threshold",
    condition: {
      id: "c-lt-1",
      operator: "isLessThan",
      leftOperand: { type: "element", value: "q2" },
      rightOperand: { type: "static", value: 100 },
    },
    expected: true,
  },
  {
    name: "isLessThan: above the threshold",
    condition: {
      id: "c-lt-2",
      operator: "isLessThan",
      leftOperand: { type: "element", value: "q2" },
      rightOperand: { type: "static", value: 10 },
    },
    expected: false,
  },
  {
    name: "isGreaterThanOrEqual: exactly at the threshold",
    condition: {
      id: "c-gte-1",
      operator: "isGreaterThanOrEqual",
      leftOperand: { type: "element", value: "q2" },
      rightOperand: { type: "static", value: 42 },
    },
    expected: true,
  },
  {
    name: "isGreaterThanOrEqual: one below the threshold",
    condition: {
      id: "c-gte-2",
      operator: "isGreaterThanOrEqual",
      leftOperand: { type: "element", value: "q2" },
      rightOperand: { type: "static", value: 43 },
    },
    expected: false,
  },
  {
    name: "isLessThanOrEqual: exactly at the threshold",
    condition: {
      id: "c-lte-1",
      operator: "isLessThanOrEqual",
      leftOperand: { type: "element", value: "q2" },
      rightOperand: { type: "static", value: 42 },
    },
    expected: true,
  },
  {
    name: "isLessThanOrEqual: one above the threshold",
    condition: {
      id: "c-lte-2",
      operator: "isLessThanOrEqual",
      leftOperand: { type: "element", value: "q2" },
      rightOperand: { type: "static", value: 41 },
    },
    expected: false,
  },

  // set membership
  {
    name: "equalsOneOf: the answer is in the list",
    condition: {
      id: "c-eqoneof-1",
      operator: "equalsOneOf",
      leftOperand: { type: "element", value: "q3" },
      rightOperand: { type: "static", value: ["opt1", "opt2"] },
    },
    expected: true,
  },
  {
    name: "equalsOneOf: the answer is not in the list",
    condition: {
      id: "c-eqoneof-2",
      operator: "equalsOneOf",
      leftOperand: { type: "element", value: "q3" },
      rightOperand: { type: "static", value: ["opt2"] },
    },
    expected: false,
  },
  {
    name: "includesAllOf: every listed choice was selected",
    condition: {
      id: "c-inall-1",
      operator: "includesAllOf",
      leftOperand: { type: "element", value: "q4" },
      rightOperand: { type: "static", value: ["opt1", "opt2"] },
    },
    expected: true,
  },
  {
    name: "includesAllOf: one listed choice is missing",
    condition: {
      id: "c-inall-2",
      operator: "includesAllOf",
      leftOperand: { type: "element", value: "q4" },
      rightOperand: { type: "static", value: ["opt1", "opt3"] },
    },
    expected: false,
  },
  {
    name: "includesOneOf: one listed choice was selected",
    condition: {
      id: "c-inone-1",
      operator: "includesOneOf",
      leftOperand: { type: "element", value: "q4" },
      rightOperand: { type: "static", value: ["opt3", "opt1"] },
    },
    expected: true,
  },
  {
    name: "includesOneOf: no listed choice was selected",
    condition: {
      id: "c-inone-2",
      operator: "includesOneOf",
      leftOperand: { type: "element", value: "q4" },
      rightOperand: { type: "static", value: ["opt3"] },
    },
    expected: false,
  },
  {
    name: "doesNotIncludeAllOf: a partial overlap still fires",
    condition: {
      id: "c-notinall-1",
      operator: "doesNotIncludeAllOf",
      leftOperand: { type: "element", value: "q4" },
      rightOperand: { type: "static", value: ["opt1", "opt3"] },
    },
    expected: true,
  },
  {
    name: "doesNotIncludeAllOf: a complete overlap does not",
    condition: {
      id: "c-notinall-2",
      operator: "doesNotIncludeAllOf",
      leftOperand: { type: "element", value: "q4" },
      rightOperand: { type: "static", value: ["opt1", "opt2"] },
    },
    expected: false,
  },
  {
    name: "doesNotIncludeOneOf: no listed choice was selected",
    condition: {
      id: "c-notinone-1",
      operator: "doesNotIncludeOneOf",
      leftOperand: { type: "element", value: "q4" },
      rightOperand: { type: "static", value: ["opt3"] },
    },
    expected: true,
  },
  {
    name: "doesNotIncludeOneOf: a listed choice was selected",
    condition: {
      id: "c-notinone-2",
      operator: "doesNotIncludeOneOf",
      leftOperand: { type: "element", value: "q4" },
      rightOperand: { type: "static", value: ["opt1"] },
    },
    expected: false,
  },
  {
    name: "isAnyOf: the answer is in the list",
    condition: {
      id: "c-anyof-1",
      operator: "isAnyOf",
      leftOperand: { type: "element", value: "q3" },
      rightOperand: { type: "static", value: ["opt1", "opt2"] },
    },
    expected: true,
  },
  {
    name: "isAnyOf: the answer is not in the list",
    condition: {
      id: "c-anyof-2",
      operator: "isAnyOf",
      leftOperand: { type: "element", value: "q3" },
      rightOperand: { type: "static", value: ["opt2"] },
    },
    expected: false,
  },

  // CTA / consent / booking states
  {
    name: "isClicked: the CTA was clicked",
    condition: {
      id: "c-clicked-1",
      operator: "isClicked",
      leftOperand: { type: "hiddenField", value: "hfStatus" },
    },
    expected: true,
  },
  {
    name: "isClicked: the CTA was skipped",
    condition: {
      id: "c-clicked-2",
      operator: "isClicked",
      leftOperand: { type: "hiddenField", value: "hfStatus" },
    },
    data: { hfStatus: "skipped" },
    expected: false,
  },
  {
    name: "isNotClicked: the CTA was clicked",
    condition: {
      id: "c-notclicked-1",
      operator: "isNotClicked",
      leftOperand: { type: "hiddenField", value: "hfStatus" },
    },
    expected: false,
  },
  {
    name: "isNotClicked: the CTA was skipped",
    condition: {
      id: "c-notclicked-2",
      operator: "isNotClicked",
      leftOperand: { type: "hiddenField", value: "hfStatus" },
    },
    data: { hfStatus: "skipped" },
    expected: true,
  },
  {
    name: "isAccepted: consent given",
    condition: {
      id: "c-accepted-1",
      operator: "isAccepted",
      leftOperand: { type: "hiddenField", value: "hfStatus" },
    },
    data: { hfStatus: "accepted" },
    expected: true,
  },
  {
    name: "isAccepted: consent not given",
    condition: {
      id: "c-accepted-2",
      operator: "isAccepted",
      leftOperand: { type: "hiddenField", value: "hfStatus" },
    },
    expected: false,
  },
  {
    name: "isBooked: a booking was made",
    condition: {
      id: "c-booked-1",
      operator: "isBooked",
      leftOperand: { type: "hiddenField", value: "hfStatus" },
    },
    data: { hfStatus: "booked" },
    expected: true,
  },
  {
    name: "isBooked: nothing was booked",
    condition: {
      id: "c-booked-2",
      operator: "isBooked",
      leftOperand: { type: "hiddenField", value: "hfEmpty" },
    },
    expected: false,
  },

  // dates
  {
    name: "isBefore: the answer precedes the bound",
    condition: {
      id: "c-before-1",
      operator: "isBefore",
      leftOperand: { type: "element", value: "q5" },
      rightOperand: { type: "static", value: "2024-01-01" },
    },
    expected: true,
  },
  {
    name: "isBefore: the answer follows the bound",
    condition: {
      id: "c-before-2",
      operator: "isBefore",
      leftOperand: { type: "element", value: "q5" },
      rightOperand: { type: "static", value: "2022-01-01" },
    },
    expected: false,
  },
  {
    name: "isAfter: the answer follows the bound",
    condition: {
      id: "c-after-1",
      operator: "isAfter",
      leftOperand: { type: "element", value: "q5" },
      rightOperand: { type: "static", value: "2022-01-01" },
    },
    expected: true,
  },
  {
    name: "isAfter: the answer precedes the bound",
    condition: {
      id: "c-after-2",
      operator: "isAfter",
      leftOperand: { type: "element", value: "q5" },
      rightOperand: { type: "static", value: "2024-01-01" },
    },
    expected: false,
  },

  // composite answers
  {
    name: "isPartiallySubmitted: one field of the group is blank",
    condition: {
      id: "c-partial-1",
      operator: "isPartiallySubmitted",
      leftOperand: { type: "hiddenField", value: "hfObject" },
    },
    data: { hfObject: { first: "Jane", last: "" } },
    expected: true,
  },
  {
    name: "isPartiallySubmitted: every field of the group is filled",
    condition: {
      id: "c-partial-2",
      operator: "isPartiallySubmitted",
      leftOperand: { type: "hiddenField", value: "hfObject" },
    },
    expected: false,
  },
  {
    name: "isCompletelySubmitted: every field of the group is filled",
    condition: {
      id: "c-complete-1",
      operator: "isCompletelySubmitted",
      leftOperand: { type: "hiddenField", value: "hfObject" },
    },
    expected: true,
  },
  {
    name: "isCompletelySubmitted: one field of the group is blank",
    condition: {
      id: "c-complete-2",
      operator: "isCompletelySubmitted",
      leftOperand: { type: "hiddenField", value: "hfObject" },
    },
    data: { hfObject: { first: "Jane", last: "" } },
    expected: false,
  },

  // presence
  {
    name: "isSet: the operand has a value",
    condition: {
      id: "c-isset-1",
      operator: "isSet",
      leftOperand: { type: "hiddenField", value: "hfText" },
    },
    expected: true,
  },
  {
    name: "isSet: the operand is unset",
    condition: {
      id: "c-isset-2",
      operator: "isSet",
      leftOperand: { type: "hiddenField", value: "hfMissing" },
    },
    expected: false,
  },
  {
    name: "isNotSet: the operand is unset",
    condition: {
      id: "c-isnotset-1",
      operator: "isNotSet",
      leftOperand: { type: "hiddenField", value: "hfMissing" },
    },
    expected: true,
  },
  {
    name: "isNotSet: the operand has a value",
    condition: {
      id: "c-isnotset-2",
      operator: "isNotSet",
      leftOperand: { type: "hiddenField", value: "hfText" },
    },
    expected: false,
  },
  {
    name: "isEmpty: the operand was cleared",
    condition: {
      id: "c-isempty-1",
      operator: "isEmpty",
      leftOperand: { type: "hiddenField", value: "hfEmpty" },
    },
    expected: true,
  },
  {
    name: "isEmpty: the operand has a value",
    condition: {
      id: "c-isempty-2",
      operator: "isEmpty",
      leftOperand: { type: "hiddenField", value: "hfText" },
    },
    expected: false,
  },
  {
    name: "isNotEmpty: the operand has a value",
    condition: {
      id: "c-isnotempty-1",
      operator: "isNotEmpty",
      leftOperand: { type: "hiddenField", value: "hfText" },
    },
    expected: true,
  },
  {
    name: "isNotEmpty: the operand was cleared",
    condition: {
      id: "c-isnotempty-2",
      operator: "isNotEmpty",
      leftOperand: { type: "hiddenField", value: "hfEmpty" },
    },
    expected: false,
  },
];

/**
 * The evaluator both engines expose. `packages/surveys/src/lib/logic.ts` and
 * `apps/web/lib/surveyLogic/utils.ts` each export `evaluateLogic` with exactly this signature, which
 * is what lets one table drive both.
 */
export type TLogicParityEvaluator = (
  localSurvey: TJsWorkspaceStateSurvey,
  data: TResponseData,
  variablesData: TResponseVariables,
  conditions: TConditionGroup,
  selectedLanguage: string,
  embeddedValues: TResponseData
) => boolean;

export const runLogicParityCase = (evaluate: TLogicParityEvaluator, parityCase: TLogicParityCase): boolean =>
  evaluate(
    PARITY_SURVEY,
    { ...PARITY_RESPONSE_DATA, ...parityCase.data },
    { ...PARITY_VARIABLE_DATA, ...parityCase.variables },
    { id: "parity-group", connector: "and", conditions: [parityCase.condition] },
    PARITY_LANGUAGE,
    { ...parityCase.embeddedValues }
  );

/**
 * Operators of `ZSurveyLogicConditionsOperator` with no case above. Both parity suites assert this is
 * empty, so adding an operator to the union without a fixture fails the tests on both sides — the
 * runtime counterpart to the `satisfies never` gate, which only proves an arm exists.
 */
export const getUncoveredLogicParityOperators = (): TSurveyLogicConditionsOperator[] => {
  const covered = new Set<TSurveyLogicConditionsOperator>(
    LOGIC_PARITY_CASES.map((parityCase) => parityCase.condition.operator)
  );
  return ZSurveyLogicConditionsOperator.options.filter((operator) => !covered.has(operator));
};
