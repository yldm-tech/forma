import { createId } from "@paralleldrive/cuid2";
import type { TFunction } from "i18next";
import type {
  TSurveyEndScreenCard,
  TSurveyHiddenFields,
  TSurveyLanguage,
  TSurveyWelcomeCard,
} from "@forma/types/surveys/types";
import { createI18nString, extractLanguageCodes } from "../../../lib/survey/i18n";

/** A new ending card: a thank-you headline and subheader and no button. Attribution stays in the "Powered by" badge, which the workspace can turn off; a default button pointing respondents to Forma would show on every customer's survey. */
export const getDefaultEndingCard = (languages: TSurveyLanguage[], t: TFunction): TSurveyEndScreenCard => {
  const languageCodes = extractLanguageCodes(languages);
  return {
    id: createId(),
    type: "endScreen",
    headline: createI18nString(t("templates.default_ending_card_headline"), languageCodes),
    subheader: createI18nString(t("templates.default_ending_card_subheader"), languageCodes),
  };
};

export const hiddenFieldsDefault: TSurveyHiddenFields = {
  enabled: true,
  fieldIds: [],
};

export const getDefaultWelcomeCard = (t: TFunction): TSurveyWelcomeCard => {
  return {
    enabled: false,
    headline: createI18nString(t("templates.default_welcome_card_headline"), []),
    subheader: createI18nString(t("templates.default_welcome_card_html"), []),
    buttonLabel: createI18nString(t("templates.default_welcome_card_button_label"), []),
    timeToFinish: false,
    showResponseCount: false,
  };
};
