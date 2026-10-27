/**
 * The card arrangements and card widths a survey's styling chooses from, and the values a survey takes when it stored none. Kept free of zod so the survey runtime (`packages/surveys`) and the public survey page (`apps/web/src/survey-page`) can import them without the schema library, which is a sizeable share of what every respondent downloads; `styling.ts` builds its schemas from the same lists and re-exports these values.
 */

/** The arrangements of a link survey's cards. "cardless" is only supported for link surveys; app surveys keep the card-based arrangements. */
export const LINK_SURVEY_CARD_ARRANGEMENTS = ["casual", "straight", "simple", "cardless"] as const;
export const APP_SURVEY_CARD_ARRANGEMENTS = ["casual", "straight", "simple"] as const;

export type TCardArrangementOptions = (typeof LINK_SURVEY_CARD_ARRANGEMENTS)[number];
export type TAppSurveyCardArrangementOptions = (typeof APP_SURVEY_CARD_ARRANGEMENTS)[number];

export interface TCardArrangement {
  linkSurveys: TCardArrangementOptions;
  appSurveys: TAppSurveyCardArrangementOptions;
}

/**
 * The card arrangement a survey takes when neither it nor its workspace stored one. It is what respondents have always seen (the runtime in packages/surveys falls back to it), so the admin shows and saves the same value: the Appearance page, the survey editor's "Add custom styles" and the survey page around the runtime all read it from here. Changing it changes how every live survey without a stored arrangement looks.
 */
export const DEFAULT_CARD_ARRANGEMENT: Readonly<TCardArrangement> = Object.freeze({
  linkSurveys: "straight",
  appSurveys: "straight",
});

export const LINK_SURVEY_CARD_WIDTHS = ["narrow", "default", "wide"] as const;
export type TLinkSurveyCardWidthOptions = (typeof LINK_SURVEY_CARD_WIDTHS)[number];

export const LINK_SURVEY_CARD_WIDTH_MAX: Record<TLinkSurveyCardWidthOptions, string> = {
  narrow: "clamp(17.5rem, 88vw, 30rem)",
  default: "clamp(20rem, 92vw, 40rem)",
  wide: "clamp(24rem, 96vw, 60rem)",
};

export const getLinkSurveyCardMaxWidth = (cardWidth?: TLinkSurveyCardWidthOptions | null): string =>
  LINK_SURVEY_CARD_WIDTH_MAX[cardWidth ?? "default"];
