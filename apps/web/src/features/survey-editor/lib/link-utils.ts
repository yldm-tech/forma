import { TJsWorkspaceStateSurvey } from "@forma/types/js";
import { TSurveyBlock } from "@forma/types/surveys/blocks";
import { TSurveyElement } from "@forma/types/surveys/elements";

export function isRTL(text: string): boolean {
  const rtlCharRegex = /[\u0591-\u07FF\uFB1D-\uFDFD\uFE70-\uFEFC]/;
  return rtlCharRegex.test(text);
}

/**
 * List of RTL language codes
 */
const RTL_LANGUAGES = ["ar", "ar-SA", "ar-EG", "ar-AE", "ar-MA", "he", "fa", "ur"];

/**
 * Returns true if the language code represents an RTL language.
 * @param survey The survey to test
 * @param languageCode The language code to test (e.g., "ar", "ar-SA", "he")
 */
export function isRTLLanguage(survey: TJsWorkspaceStateSurvey, languageCode: string): boolean {
  if (survey.languages.length === 0) {
    if (survey.welcomeCard.enabled) {
      const welcomeCardHeadline = survey.welcomeCard.headline?.[languageCode];
      if (welcomeCardHeadline) {
        return isRTL(welcomeCardHeadline);
      }
    }

    const questions = getElementsFromSurveyBlocks(survey.blocks);
    for (const question of questions) {
      const questionHeadline = question.headline[languageCode];

      // the first non-empty question headline is the survey direction
      if (questionHeadline) {
        return isRTL(questionHeadline);
      }
    }
    return false;
  } else {
    const code =
      languageCode === "default"
        ? survey.languages.find((language) => language.default)?.language.code
        : languageCode;
    const baseCode = code?.split("-")[0]?.toLowerCase() ?? "en";
    return RTL_LANGUAGES.some((rtl) => rtl.toLowerCase().startsWith(baseCode));
  }
}

/**
 * Derives a flat array of elements from the survey's blocks structure.
 * @param blocks The blocks array
 * @returns An array of TSurveyElement (pure elements without block-level properties)
 */
export const getElementsFromSurveyBlocks = (blocks: TSurveyBlock[]): TSurveyElement[] =>
  blocks.flatMap((block) => block.elements);
