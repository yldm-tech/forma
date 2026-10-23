import {
  RESERVED_FIELD_CATALOG,
  type TEmbeddedFieldsSurvey,
  type TEmbeddedValueResponse,
  dropShadowedReservedEntries,
  getSurveyEmbeddedFields,
  listShadowingNames,
  mergeReservedValues,
  projectReservedValues,
} from "@forma/types/embedded-data-resolver";
import type { TResponseData } from "@forma/types/responses";
import type { TSurveyBlock } from "@forma/types/surveys/blocks";
import type { TSurveyElement } from "@forma/types/surveys/elements";
import type { TSurveyLanguage } from "@forma/types/surveys/types";
import type { JSX } from "react";
import type { TSurveyDateFormatMap } from "../../../lib/survey/date-display";
import { getElementsFromBlocks } from "../../survey-editor/lib/client-utils";

// How the response views render what a response holds: forma2's getLanguageCode, getSurveyDateFormatMap, buildServerEmbeddedValues and renderHyperlinkedContent.

/** The survey language a response was given in, as the key of its translated labels: "default" for the default language, a language the survey does not have, or none. */
export const getLanguageCode = (surveyLanguages: TSurveyLanguage[], languageCode: string | null): string => {
  if (!surveyLanguages?.length || !languageCode) return "default";
  const language = surveyLanguages.find(
    (surveyLanguage) => surveyLanguage.language.code.toLowerCase() === languageCode.toLowerCase()
  );
  return language?.default ? "default" : (language?.language.code ?? "default");
};

/** The display format of each date element, for recalled dates. */
export const getSurveyDateFormatMap = (elements: TSurveyElement[]): TSurveyDateFormatMap =>
  elements.reduce<TSurveyDateFormatMap>((dateFormats, element) => {
    if (element.type === "date") dateFormats[element.id] = element.format;
    return dateFormats;
  }, {});

/** Recall's lookup map for a stored response: every reserved field the survey does not declare itself, projected, then shadowed by the response's own answers. */
export const buildServerEmbeddedValues = (
  response: TEmbeddedValueResponse,
  survey: TEmbeddedFieldsSurvey & { blocks: TSurveyBlock[] }
): TResponseData =>
  mergeReservedValues(
    projectReservedValues(
      dropShadowedReservedEntries(
        RESERVED_FIELD_CATALOG,
        listShadowingNames(
          getSurveyEmbeddedFields(survey),
          getElementsFromBlocks(survey.blocks).map((element) => element.id)
        )
      ),
      response
    ),
    response.data
  );

const URL_PATTERN =
  /(https?:\/\/(?:www\.)?[-a-zA-Z0-9@:%._+~#=]{1,256}\.[a-zA-Z0-9()]{1,6}\b(?:[-a-zA-Z0-9()@:%_+.~#?&//=]*[-a-zA-Z0-9@%_+~#//=])?)/g;

const isValidUrl = (url: string): boolean => {
  try {
    new URL(url);
    return true;
  } catch {
    return false;
  }
};

/** An answer's text with its web addresses as links that open in a new tab. */
export const renderHyperlinkedContent = (data: string): JSX.Element[] =>
  data.split(URL_PATTERN).map((part, index) =>
    // The split alternates text and matched addresses; the position is the part's identity.
    // oxlint-disable-next-line react/no-array-index-key -- see above
    new RegExp(`^${URL_PATTERN.source}$`).test(part) && isValidUrl(part) ? (
      // oxlint-disable-next-line react/no-array-index-key -- see above
      <a key={index} href={part} target="_blank" rel="noopener noreferrer" className="text-blue-500">
        {part}
      </a>
    ) : (
      // oxlint-disable-next-line react/no-array-index-key -- see above
      <span key={index}>{part}</span>
    )
  );
