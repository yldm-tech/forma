import { createId } from "@paralleldrive/cuid2";
import type { TFunction } from "i18next";
import type {
  TSurveyEndScreenCard,
  TSurveyHiddenFields,
  TSurveyLanguage,
  TSurveyWelcomeCard,
} from "@forma/types/surveys/types";
import { createI18nString, extractLanguageCodes } from "../../../lib/survey/i18n";

export const getDefaultEndingCard = (languages: TSurveyLanguage[], t: TFunction): TSurveyEndScreenCard => {
  const languageCodes = extractLanguageCodes(languages);
  return {
    id: createId(),
    type: "endScreen",
    headline: createI18nString(t("templates.default_ending_card_headline"), languageCodes),
    subheader: createI18nString(t("templates.default_ending_card_subheader"), languageCodes),
    buttonLabel: createI18nString(t("templates.default_ending_card_button_label"), languageCodes),
    buttonLink:
      "https://forma.yldm.ai?utm_source=forma-app&utm_medium=survey&utm_campaign=default_ending_cta",
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
