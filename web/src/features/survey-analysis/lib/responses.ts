import type { TContactAttributes } from "@forma/types/contact-attribute";
import type { TResponseContact, TResponseDataValue } from "@forma/types/responses";
import type {
  TSurveyElement,
  TSurveyMultipleChoiceElement,
  TSurveyPictureSelectionElement,
  TSurveyRankingElement,
} from "@forma/types/surveys/elements";

// forma2's response helpers shared by the summary, the response table and the response card: choice ids behind stored answers, an answer as text, and who answered.

/**
 * Extracts choice IDs from response values for multiple choice elements
 * @param responseValue - The response value (string for single choice, array for multi choice)
 * @param element - The survey element containing choices
 * @param language - The language to match against (defaults to "default")
 * @returns Array of choice IDs
 */
export const extractChoiceIdsFromResponse = (
  responseValue: TResponseDataValue,
  element: TSurveyElement,
  language: string = "default"
): string[] => {
  if (
    element.type !== "multipleChoiceMulti" &&
    element.type !== "multipleChoiceSingle" &&
    element.type !== "ranking" &&
    element.type !== "pictureSelection"
  ) {
    return [];
  }

  const isPictureSelection = element.type === "pictureSelection";

  if (!responseValue) {
    return [];
  }

  // For picture selection elements, the response value is already choice ID(s)
  if (isPictureSelection) {
    if (Array.isArray(responseValue)) {
      // Multi-selection: array of choice IDs
      return responseValue.filter((id): id is string => typeof id === "string");
    } else if (typeof responseValue === "string") {
      // Single selection: single choice ID
      return [responseValue];
    }
    return [];
  }

  const defaultLanguage = language ?? "default";

  // Helper function to find choice by label - eliminates duplication
  const findChoiceByLabel = (choiceLabel: string): string | null => {
    const targetChoice = element.choices.find((c) => {
      // Try exact language match first
      if (c.label[defaultLanguage] === choiceLabel) {
        return true;
      }
      // Fall back to checking all language values
      return Object.values(c.label).includes(choiceLabel);
    });
    return targetChoice?.id || "other";
  };

  if (Array.isArray(responseValue)) {
    // Multiple choice case - response is an array of selected choice labels Filter out empty string sentinel used as "other" marker in multipleChoiceMulti
    return responseValue
      .filter((v) => v !== "")
      .map(findChoiceByLabel)
      .filter((choiceId): choiceId is string => choiceId !== null);
  } else if (typeof responseValue === "string") {
    // Single choice case - response is a single choice label
    const choiceId = findChoiceByLabel(responseValue);
    return choiceId ? [choiceId] : [];
  }

  return [];
};

export const getChoiceIdByValue = (
  value: string,
  element: TSurveyMultipleChoiceElement | TSurveyRankingElement | TSurveyPictureSelectionElement
) => {
  if (element.type === "pictureSelection") {
    return element.choices.find((choice) => choice.imageUrl === value)?.id ?? "other";
  }

  return element.choices.find((choice) => choice.label.default === value)?.id ?? "other";
};

/** forma2's processResponseData: an answer as one line of text, the way the table and the exports render it. */
export const processResponseData = (responseData: TResponseDataValue): string => {
  switch (typeof responseData) {
    case "string":
      return responseData;

    case "number":
      return responseData.toString();

    case "object":
      if (Array.isArray(responseData)) {
        responseData = responseData
          .filter((item) => item !== null && item !== undefined && item !== "")
          .join("; ");
        return responseData;
      } else {
        const formattedString = Object.entries(responseData)
          .filter(([_, value]) => value !== "")
          .map(([key, value]) => `${key}: ${value}`)
          .join("\n");
        return formattedString;
      }

    default:
      return "";
  }
};

/** forma2's getContactIdentifier: the contact's email attribute, else its userId, else nothing. */
export const getContactIdentifier = (
  contact: TResponseContact | null,
  contactAttributes: TContactAttributes | null
): string => contactAttributes?.email || contact?.userId || "";
