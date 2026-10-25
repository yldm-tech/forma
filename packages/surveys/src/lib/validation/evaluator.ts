import type { TFunction } from "i18next";
import type { TResponseData, TResponseDataValue } from "@forma/types/responses";
import type { TSurveyElement } from "@forma/types/surveys/elements";
import { DATE_TIME_ANSWER_PATTERN } from "@forma/types/surveys/date-answer";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import { isSliderValueOnScale } from "@forma/types/surveys/slider";
import type {
  TAddressField,
  TContactInfoField,
  TValidationError,
  TValidationErrorMap,
  TValidationResult,
  TValidationRule,
} from "@forma/types/surveys/validation-rules";
import { getLocalizedValue, getTranslations } from "@/lib/i18n";
import { validators } from "./validators";

/**
 * Check if a value is empty
 */
const isEmpty = (value: TResponseDataValue): boolean => {
  return (
    value === undefined ||
    value === null ||
    value === "" ||
    (Array.isArray(value) && value.length === 0) ||
    (typeof value === "object" && !Array.isArray(value) && Object.keys(value as object).length === 0)
  );
};

/**
 * The message for a required element left unanswered, worded for what the respondent has to do with that element type ("select an option", "choose a rating"). Text inputs, and the text parts of other elements (an "Other" option's text, an address or contact field), keep "Please fill out this field".
 */
const getRequiredMessage = (t: TFunction, elementType?: TSurveyElementTypeEnum): string => {
  switch (elementType) {
    case TSurveyElementTypeEnum.MultipleChoiceSingle:
    case TSurveyElementTypeEnum.MultipleChoiceMulti:
    case TSurveyElementTypeEnum.PictureSelection:
      return t("errors.select_an_option");
    case TSurveyElementTypeEnum.Rating:
    case TSurveyElementTypeEnum.NPS:
    case TSurveyElementTypeEnum.CSAT:
    case TSurveyElementTypeEnum.CES:
      return t("errors.choose_a_rating");
    case TSurveyElementTypeEnum.Ranking:
      return t("errors.rank_at_least_one_option");
    case TSurveyElementTypeEnum.Matrix:
      return t("errors.answer_at_least_one_row");
    case TSurveyElementTypeEnum.Consent:
      return t("errors.accept_to_continue");
    case TSurveyElementTypeEnum.FileUpload:
      return t("errors.upload_a_file");
    case TSurveyElementTypeEnum.Date:
      return t("errors.select_a_date");
    case TSurveyElementTypeEnum.Slider:
      return t("slider.choose_a_value");
    default:
      return t("errors.please_fill_out_this_field");
  }
};

/**
 * Create a required field error. Without an element type it is the text-field message.
 */
const createRequiredError = (t: TFunction, elementType?: TSurveyElementTypeEnum): TValidationError => {
  return {
    ruleId: "required",
    ruleType: "minLength", // Structural field only - required is not a validation rule
    message: getRequiredMessage(t, elementType),
  } as TValidationError;
};

/**
 * Create an invalid-option error (value not among a choice element's configured options)
 */
const createInvalidOptionError = (t: TFunction): TValidationError => {
  return {
    ruleId: "invalidOption",
    ruleType: "minLength", // Structural field only - choice membership is not a configurable rule
    message: t("errors.invalid_format"),
  } as TValidationError;
};

/**
 * Get field label for address/contact info elements
 */
const getFieldLabel = (
  element: TSurveyElement,
  field: TAddressField | TContactInfoField | undefined,
  languageCode: string
): string | undefined => {
  if (!field) return undefined;

  if (element.type === TSurveyElementTypeEnum.Address && "addressLine1" in element) {
    const fieldConfig = element[field as TAddressField];
    if (fieldConfig && "placeholder" in fieldConfig) {
      return getLocalizedValue(fieldConfig.placeholder, languageCode);
    }
  }

  if (element.type === TSurveyElementTypeEnum.ContactInfo && "firstName" in element) {
    const fieldConfig = element[field as TContactInfoField];
    if (fieldConfig && "placeholder" in fieldConfig) {
      return getLocalizedValue(fieldConfig.placeholder, languageCode);
    }
  }

  return undefined;
};

/**
 * Get default error message from rule or validator
 */
const getDefaultErrorMessage = (
  rule: TValidationRule,
  element: TSurveyElement,
  languageCode: string,
  t: TFunction
): string => {
  const validator = validators[rule.type];
  if (!validator) {
    return t("errors.invalid_format");
  }

  const baseMessage = validator.getDefaultMessage(rule.params, element, t);

  // For field-specific validation, prepend the field name
  if (rule.field) {
    const fieldLabel = getFieldLabel(element, rule.field, languageCode);
    if (fieldLabel) {
      return `${fieldLabel}: ${baseMessage}`;
    }
  }

  return baseMessage;
};

/**
 * Validate required field for ranking elements
 */
const validateRequiredRanking = (value: TResponseDataValue, t: TFunction): TValidationError | null => {
  const isValueArray = Array.isArray(value);
  const atLeastOneRanked = isValueArray && value.length >= 1;
  if (isEmpty(value) || !atLeastOneRanked) {
    return createRequiredError(t, TSurveyElementTypeEnum.Ranking);
  }
  return null;
};

/**
 * Validate required field for matrix elements
 * Required means: at least 1 row must be answered
 */
const validateRequiredMatrix = (
  value: TResponseDataValue,
  element: TSurveyElement,
  t: TFunction
): TValidationError | null => {
  if (isEmpty(value)) {
    return createRequiredError(t, TSurveyElementTypeEnum.Matrix);
  }
  if (typeof value === "object" && !Array.isArray(value) && value !== null && "rows" in element) {
    const answeredRows = Object.values(value).filter((v) => v !== "" && v !== null && v !== undefined).length;
    // Required means at least 1 row must be answered
    if (answeredRows === 0) {
      return createRequiredError(t, TSurveyElementTypeEnum.Matrix);
    }
  }
  return null;
};

const validateMultiSelectOtherValue = (
  element: TSurveyElement,
  value: TResponseDataValue,
  t: TFunction
): TValidationError | null => {
  if (element.type !== TSurveyElementTypeEnum.MultipleChoiceMulti || !Array.isArray(value)) {
    return null;
  }

  const hasOtherOption = "choices" in element && element.choices.some((choice) => choice.id === "other");
  if (!hasOtherOption) {
    return null;
  }

  const sentinelIndex = value.indexOf("");
  if (sentinelIndex === -1) {
    return null;
  }

  const otherText = value[sentinelIndex + 1];
  if (typeof otherText !== "string" || otherText.trim() === "") {
    return createRequiredError(t);
  }

  return null;
};

const validateSingleSelectOtherValue = (
  element: TSurveyElement,
  value: TResponseDataValue,
  languageCode: string,
  t: TFunction
): TValidationError | null => {
  if (element.type !== TSurveyElementTypeEnum.MultipleChoiceSingle || typeof value !== "string") {
    return null;
  }

  const hasOtherOption = "choices" in element && element.choices.some((choice) => choice.id === "other");
  if (!hasOtherOption || (element.required && value === "")) {
    return null;
  }

  const knownChoiceIds = element.choices.filter((choice) => choice.id !== "other").map((choice) => choice.id);
  if (knownChoiceIds.includes(value)) {
    return null;
  }

  const knownChoiceLabels = element.choices
    .filter((choice) => choice.id !== "other")
    .map((choice) => getLocalizedValue(choice.label, languageCode));
  if (knownChoiceLabels.includes(value)) {
    return null;
  }

  if (value.trim() === "") {
    return createRequiredError(t);
  }

  return null;
};

/**
 * Reject answer values that are not among a choice element's configured options.
 *
 * Choice elements that expose an "Other" option intentionally accept free text, so this
 * check only applies when the element has NO "Other" option — there a legitimate client can
 * only ever submit a configured choice (by id or localized label), so any other value is
 * tampered/garbage input submitted directly to the API. Empty values are deferred to the
 * required check.
 */
const validateChoiceMembership = (
  element: TSurveyElement,
  value: TResponseDataValue,
  languageCode: string,
  t: TFunction
): TValidationError | null => {
  if (
    element.type !== TSurveyElementTypeEnum.MultipleChoiceSingle &&
    element.type !== TSurveyElementTypeEnum.MultipleChoiceMulti
  ) {
    return null;
  }

  // Defensive: a malformed survey element could be missing `choices` at runtime; match the
  // other choice validators and bail rather than throw.
  if (!("choices" in element) || !Array.isArray(element.choices)) {
    return null;
  }

  const hasOtherOption = element.choices.some((choice) => choice.id === "other");
  if (hasOtherOption || isEmpty(value)) {
    return null;
  }

  const knownValues = new Set<string>();
  for (const choice of element.choices) {
    knownValues.add(choice.id);
    const label = getLocalizedValue(choice.label, languageCode);
    if (label) {
      knownValues.add(label);
    }
  }

  const submittedValues = Array.isArray(value) ? value : [value];
  for (const submitted of submittedValues) {
    // Tolerate stray empty/sentinel entries; emptiness is the required check's concern.
    if (submitted === "") {
      continue;
    }
    if (typeof submitted !== "string" || !knownValues.has(submitted)) {
      return createInvalidOptionError(t);
    }
  }

  return null;
};

/**
 * A slider answer must be a position of its scale: a number from min to max, a whole number of steps from min. The slider itself only produces such numbers, so anything else came straight to the API. Empty values are deferred to the required check. The server repeats this check (apps/server/internal/response/answers).
 */
const validateSliderValue = (
  element: TSurveyElement,
  value: TResponseDataValue,
  t: TFunction
): TValidationError | null => {
  if (element.type !== TSurveyElementTypeEnum.Slider || isEmpty(value)) {
    return null;
  }
  return isSliderValueOnScale(element, value) ? null : createInvalidOptionError(t);
};

/**
 * A date element that asks for the time of day takes a date and a time. The picker stores the day alone while the time is still missing, which is what this names; the server answers such a value with the invalid-format message.
 */
const validateDateTimeValue = (
  element: TSurveyElement,
  value: TResponseDataValue,
  t: TFunction
): TValidationError | null => {
  if (element.type !== TSurveyElementTypeEnum.Date || !element.includeTime || isEmpty(value)) {
    return null;
  }
  if (typeof value === "string" && DATE_TIME_ANSWER_PATTERN.test(value)) {
    return null;
  }
  return {
    ruleId: "invalidOption",
    ruleType: "minLength", // Structural field only - the time of day is not a configurable rule
    message: t("date_time.select_a_time"),
  } as TValidationError;
};

/**
 * Check required field validation
 */
const checkRequiredField = (
  element: TSurveyElement,
  value: TResponseDataValue,
  t: TFunction
): TValidationError | null => {
  if (!element.required) {
    return null;
  }

  // CTA elements never block progression (informational only)
  if (element.type === TSurveyElementTypeEnum.CTA) {
    return null;
  }

  if (element.type === TSurveyElementTypeEnum.Ranking) {
    return validateRequiredRanking(value, t);
  }

  if (element.type === TSurveyElementTypeEnum.Matrix) {
    return validateRequiredMatrix(value, element, t);
  }

  // A single select stores "" while its "Other" option is picked and the text is still empty: what is missing is that text.
  if (
    element.type === TSurveyElementTypeEnum.MultipleChoiceSingle &&
    value === "" &&
    "choices" in element &&
    element.choices.some((choice) => choice.id === "other")
  ) {
    return createRequiredError(t);
  }

  if (isEmpty(value)) {
    return createRequiredError(t, element.type);
  }

  return null;
};

/**
 * Add implicit validation rules for OpenText elements based on inputType
 */
const addImplicitOpenTextRules = (element: TSurveyElement, rules: TValidationRule[]): TValidationRule[] => {
  if (element.type !== TSurveyElementTypeEnum.OpenText || !("inputType" in element)) {
    return rules;
  }

  const inputType = element.inputType;
  const hasRule = (type: string) => rules.some((r) => r.type === type);

  if (inputType === "email" && !hasRule("email")) {
    rules.push({
      id: "__implicit_email__",
      type: "email",
      params: {},
    } as TValidationRule);
  } else if (inputType === "url" && !hasRule("url")) {
    rules.push({
      id: "__implicit_url__",
      type: "url",
      params: {},
    } as TValidationRule);
  } else if (inputType === "phone" && !hasRule("phone")) {
    rules.push({
      id: "__implicit_phone__",
      type: "phone",
      params: {},
    } as TValidationRule);
  }

  // The character limit's minimum. The input used to carry it as a minLength attribute, which the browser enforced with its own tooltip, in the browser's language and outside the survey's error styling; as a rule it gets the localized inline message every other check shows. The runtime only applies the limit to text inputs (OpenTextElement passes charLimit for inputType "text" alone), and only while it is enabled.
  const charLimit = "charLimit" in element ? element.charLimit : undefined;
  if (inputType === "text" && charLimit?.enabled && typeof charLimit.min === "number" && charLimit.min > 0) {
    rules.push({
      id: "__implicit_char_limit_min__",
      type: "minLength",
      params: { min: charLimit.min },
    } as TValidationRule);
  }

  return rules;
};

/**
 * Add implicit validation rules for ContactInfo elements
 */
const addImplicitContactInfoRules = (
  element: TSurveyElement,
  rules: TValidationRule[]
): TValidationRule[] => {
  if (element.type !== TSurveyElementTypeEnum.ContactInfo) {
    return rules;
  }

  const contactInfoElement = element;
  const hasFieldRule = (type: string, field: string) =>
    rules.some((r) => r.type === type && r.field === field);

  if (contactInfoElement.email?.show && !hasFieldRule("email", "email")) {
    rules.push({
      id: "__implicit_email_field__",
      type: "email",
      field: "email",
      params: {},
    } as TValidationRule);
  }

  if (contactInfoElement.phone?.show && !hasFieldRule("phone", "phone")) {
    rules.push({
      id: "__implicit_phone_field__",
      type: "phone",
      field: "phone",
      params: {},
    } as TValidationRule);
  }

  return rules;
};

const ADDRESS_FIELD_ORDER: TAddressField[] = [
  "addressLine1",
  "addressLine2",
  "city",
  "state",
  "zip",
  "country",
];
const CONTACT_INFO_FIELD_ORDER: TContactInfoField[] = ["firstName", "lastName", "email", "phone", "company"];

interface TFormFieldConfig {
  show?: boolean;
  required?: boolean;
}

/** The sub-fields of an address or contact-info element in their response order, with their show/required configuration; empty for any other element. */
const getFormFields = (
  element: TSurveyElement
): { id: TAddressField | TContactInfoField; config: TFormFieldConfig }[] => {
  if (element.type === TSurveyElementTypeEnum.Address) {
    return ADDRESS_FIELD_ORDER.map((id) => ({ id, config: element[id] as TFormFieldConfig }));
  }
  if (element.type === TSurveyElementTypeEnum.ContactInfo) {
    return CONTACT_INFO_FIELD_ORDER.map((id) => ({ id, config: element[id] as TFormFieldConfig }));
  }
  return [];
};

/**
 * The visible sub-fields of an address or contact-info element that must be filled, as the form renders them (FormField in survey-ui): a field marked required, or every visible field when the element is required and none is marked.
 */
const getRequiredFormFields = (element: TSurveyElement): Set<string> => {
  const visible = getFormFields(element).filter(({ config }) => config?.show !== false);
  const noneMarked = visible.every(({ config }) => !config?.required);
  return new Set(
    visible.filter(({ config }) => config?.required || (noneMarked && element.required)).map(({ id }) => id)
  );
};

const getFormFieldValue = (element: TSurveyElement, value: TResponseDataValue, fieldId: string): string => {
  if (!Array.isArray(value)) return "";
  const index = getFormFields(element).findIndex(({ id }) => id === fieldId);
  const fieldValue = index >= 0 ? value[index] : undefined;
  return typeof fieldValue === "string" ? fieldValue : "";
};

/**
 * Get field value for address/contact info elements
 */
const getFieldValue = (
  rule: TValidationRule,
  element: TSurveyElement,
  elementValue: TResponseDataValue
): TResponseDataValue => {
  if (!rule.field) {
    return elementValue;
  }

  if (element.type === TSurveyElementTypeEnum.Address && Array.isArray(elementValue)) {
    const fieldIndex = ADDRESS_FIELD_ORDER.indexOf(rule.field as TAddressField);
    if (fieldIndex >= 0 && fieldIndex < elementValue.length) {
      return elementValue[fieldIndex] ?? "";
    }
  }

  if (element.type === TSurveyElementTypeEnum.ContactInfo && Array.isArray(elementValue)) {
    const fieldIndex = CONTACT_INFO_FIELD_ORDER.indexOf(rule.field as TContactInfoField);
    if (fieldIndex >= 0 && fieldIndex < elementValue.length) {
      return elementValue[fieldIndex] ?? "";
    }
  }

  return "";
};

/**
 * Execute validation rules with OR logic
 */
const executeOrLogic = (
  rules: TValidationRule[],
  element: TSurveyElement,
  value: TResponseDataValue,
  languageCode: string,
  initialErrors: TValidationError[],
  t: TFunction
): TValidationResult => {
  const ruleResults: TValidationError[] = [];

  for (const rule of rules) {
    const validator = validators[rule.type];
    if (!validator) {
      console.warn(`Unknown validation rule type: ${rule.type}`);
      continue;
    }

    const valueToValidate = getFieldValue(rule, element, value);
    const checkResult = validator.check(valueToValidate, rule.params, element);

    if (checkResult.valid) {
      return { valid: initialErrors.length === 0, errors: initialErrors };
    }

    const message = getDefaultErrorMessage(rule, element, languageCode, t);
    ruleResults.push({
      ruleId: rule.id,
      ruleType: rule.type,
      message,
    });
  }

  if (ruleResults.length === 0) {
    return { valid: initialErrors.length === 0, errors: initialErrors };
  }

  return { valid: false, errors: [...initialErrors, ...ruleResults] };
};

/**
 * Execute validation rules with AND logic
 */
const executeAndLogic = (
  rules: TValidationRule[],
  element: TSurveyElement,
  value: TResponseDataValue,
  languageCode: string,
  initialErrors: TValidationError[],
  t: TFunction
): TValidationResult => {
  const errors = [...initialErrors];

  for (const rule of rules) {
    const validator = validators[rule.type];
    if (!validator) {
      console.warn(`Unknown validation rule type: ${rule.type}`);
      continue;
    }

    const valueToValidate = getFieldValue(rule, element, value);
    const checkResult = validator.check(valueToValidate, rule.params, element);

    if (!checkResult.valid) {
      const message = getDefaultErrorMessage(rule, element, languageCode, t);
      errors.push({
        ruleId: rule.id,
        ruleType: rule.type,
        message,
      });
    }
  }

  return { valid: errors.length === 0, errors };
};

/**
 * Single entrypoint for validating an element's response value.
 * Called by block-conditional.tsx during form submission.
 *
 * @param element - The survey element being validated
 * @param value - The response value for this element
 * @param languageCode - Current language code for error messages
 * @returns Validation result with valid flag and array of errors
 */
export const validateElementResponse = (
  element: TSurveyElement,
  value: TResponseDataValue,
  languageCode: string
): TValidationResult => {
  const errors: TValidationError[] = [];
  // Always create translation function from surveys package's i18n instance
  const t: TFunction = getTranslations(languageCode);

  // Check if element is required (separate from validation rules)
  const requiredError = checkRequiredField(element, value, t);
  if (requiredError) {
    errors.push(requiredError);
  } else if (getEmptyRequiredFormFields(element, value).length > 0) {
    // A started address or contact form with a required sub-field left empty; the form's own required attributes caught this only after every other rule had passed.
    errors.push(createRequiredError(t));
  }

  const singleSelectOtherError = validateSingleSelectOtherValue(element, value, languageCode, t);
  if (singleSelectOtherError) {
    errors.push(singleSelectOtherError);
  }

  const multiSelectOtherError = validateMultiSelectOtherValue(element, value, t);
  if (multiSelectOtherError) {
    errors.push(multiSelectOtherError);
  }

  const invalidOptionError = validateChoiceMembership(element, value, languageCode, t);
  if (invalidOptionError) {
    errors.push(invalidOptionError);
  }

  const sliderError = validateSliderValue(element, value, t);
  if (sliderError) {
    errors.push(sliderError);
  }

  const dateTimeError = validateDateTimeValue(element, value, t);
  if (dateTimeError) {
    errors.push(dateTimeError);
  }

  // Validation rules apply to matrix elements regardless of required status

  // Get validation rules
  const validation = (
    element as TSurveyElement & { validation?: { rules?: TValidationRule[]; logic?: "and" | "or" } }
  ).validation;
  const authorRules: TValidationRule[] = [...(validation?.rules ?? [])];

  // Implicit rules come from the element type (an email-typed open text must hold an address), not
  // from the author, and the editor hides them as redundant. They are therefore always ANDed: folded
  // into the author's group under `logic: "or"` they become an alternative that the input satisfies
  // by construction, which short-circuits executeOrLogic and silently voids every rule the author
  // did write.
  // Fed a copy of the author's rules so the helpers' "already has this rule" checks still see them;
  // whatever they append past that point is the implicit set.
  const withImplicit = addImplicitContactInfoRules(
    element,
    addImplicitOpenTextRules(element, [...authorRules])
  );
  const implicitRules: TValidationRule[] = withImplicit.slice(authorRules.length);

  if (authorRules.length === 0 && implicitRules.length === 0) {
    return { valid: errors.length === 0, errors };
  }

  const validationLogic = validation?.logic ?? "and";

  const authorResult =
    authorRules.length === 0
      ? { valid: errors.length === 0, errors }
      : validationLogic === "or"
        ? executeOrLogic(authorRules, element, value, languageCode, errors, t)
        : executeAndLogic(authorRules, element, value, languageCode, errors, t);

  if (implicitRules.length === 0) {
    return authorResult;
  }

  const implicitResult = executeAndLogic(implicitRules, element, value, languageCode, [], t);

  return {
    valid: authorResult.valid && implicitResult.valid,
    errors: [...authorResult.errors, ...implicitResult.errors],
  };
};

const getEmptyRequiredFormFields = (element: TSurveyElement, value: TResponseDataValue): string[] =>
  [...getRequiredFormFields(element)].filter(
    (fieldId) => getFormFieldValue(element, value, fieldId).trim() === ""
  );

/**
 * The failing sub-fields of an address or contact-info element, each with its own message, so the form marks and describes only those fields instead of every input. Empty for any other element, and for an element whose response is valid.
 *
 * A required field left empty gets the required message; a field whose rule fails gets that rule's message, without the field-name prefix the element-level message carries.
 */
export const getFormFieldErrors = (
  element: TSurveyElement,
  value: TResponseDataValue,
  languageCode: string
): Record<string, string> => {
  if (getFormFields(element).length === 0) return {};
  if (validateElementResponse(element, value, languageCode).valid) return {};

  const t: TFunction = getTranslations(languageCode);
  const fieldErrors: Record<string, string> = {};
  for (const fieldId of getEmptyRequiredFormFields(element, value)) {
    fieldErrors[fieldId] = t("errors.please_fill_out_this_field");
  }

  const validation = (
    element as TSurveyElement & { validation?: { rules?: TValidationRule[]; logic?: "and" | "or" } }
  ).validation;
  const authorRules = validation?.rules ?? [];
  const passes = (rule: TValidationRule) =>
    validators[rule.type]?.check(getFieldValue(rule, element, value), rule.params, element).valid ?? true;
  // Under "or" one passing author rule satisfies them all, so none of them marks a field.
  const authorRulesSatisfied = validation?.logic === "or" && authorRules.some(passes);
  const implicitRules = addImplicitContactInfoRules(element, [...authorRules]).slice(authorRules.length);
  const rules = authorRulesSatisfied ? implicitRules : [...authorRules, ...implicitRules];
  for (const rule of rules) {
    const validator = validators[rule.type];
    if (!rule.field || !validator || fieldErrors[rule.field]) continue;
    if (!passes(rule)) {
      fieldErrors[rule.field] = validator.getDefaultMessage(rule.params, element, t);
    }
  }
  return fieldErrors;
};

/**
 * Validate all elements in a block, returning an error map.
 *
 * @param elements - Array of elements to validate
 * @param responses - Response data keyed by element ID
 * @param languageCode - Current language code for error messages
 * @returns Map of element IDs to their validation errors
 */
export const validateBlockResponses = (
  elements: TSurveyElement[],
  responses: TResponseData,
  languageCode: string
): TValidationErrorMap => {
  const errorMap: TValidationErrorMap = {};
  for (const element of elements) {
    const result = validateElementResponse(element, responses[element.id], languageCode);
    if (!result.valid) {
      errorMap[element.id] = result.errors;
    }
  }

  return errorMap;
};

/**
 * Get the first error message for an element from the error map.
 * Useful for UI components that only display one error at a time.
 *
 * @param errorMap - The validation error map
 * @param elementId - The element ID to get error for
 * @returns The first error message or undefined
 */
export const getFirstErrorMessage = (
  errorMap: TValidationErrorMap,
  elementId: string
): string | undefined => {
  const errors = errorMap[elementId];
  return errors?.[0]?.message;
};
