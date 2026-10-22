import type { TJsWorkspaceStateSurvey } from "@forma/types/js";
import type { TFunction } from "i18next";

// forma2's previewSurvey (apps/web/lib/templates.ts): the two-block survey the theme settings render with the workspace's styling. Ids are forma2's, so a preview never collides with a real survey.

const text = (value: string) => ({ default: value });

/** The preview survey as the respondent runtime reads it, of the given type. */
export const previewSurvey = (
  workspaceName: string,
  t: TFunction,
  type: "link" | "app"
): TJsWorkspaceStateSurvey =>
  ({
    id: "cltxxaa6x0000g8hacxdxejeu",
    name: t("templates.preview_survey_name"),
    type,
    status: "inProgress",
    welcomeCard: {
      enabled: false,
      headline: text(t("templates.preview_survey_welcome_card_headline")),
      timeToFinish: false,
      showResponseCount: false,
    },
    styling: null,
    segment: null,
    questions: [],
    blocks: [
      {
        id: "cltxxaa6x0000g8hacxdxeje1",
        name: t("templates.block_1"),
        elements: [
          {
            id: "rjpu42ps6dzirsn9ds6eydgt",
            type: "multipleChoiceSingle",
            headline: text(t("templates.preview_survey_question_2_headline")),
            subheader: text(t("templates.preview_survey_question_2_subheader")),
            choices: [
              {
                id: "x6wty2s72v7vd538aadpurqx",
                label: text(t("templates.preview_survey_question_2_choice_1_label")),
              },
              {
                id: "fbcj4530t2n357ymjp2h28d6",
                label: text(t("templates.preview_survey_question_2_choice_2_label")),
              },
            ],
            shuffleOption: "none",
            required: true,
            isDraft: true,
          },
          {
            id: "preview-open-text-01",
            type: "openText",
            inputType: "text",
            headline: text(t("templates.preview_survey_question_open_text_headline")),
            subheader: text(t("templates.preview_survey_question_open_text_subheader")),
            placeholder: text(t("templates.preview_survey_question_open_text_placeholder")),
            required: false,
            charLimit: { enabled: false },
            isDraft: true,
          },
        ],
        buttonLabel: text(t("templates.next")),
        backButtonLabel: text(t("templates.preview_survey_question_2_back_button_label")),
      },
      {
        id: "cltxxaa6x0000g8hacxdxeje2",
        name: t("templates.block_2"),
        elements: [
          {
            id: "lbdxozwikh838yc6a8vbwuju",
            type: "rating",
            headline: text(t("templates.preview_survey_question_1_headline", { workspaceName })),
            subheader: text(t("templates.preview_survey_question_1_subheader")),
            scale: "star",
            range: 5,
            required: true,
            isColorCodingEnabled: false,
            lowerLabel: text(t("templates.preview_survey_question_1_lower_label")),
            upperLabel: text(t("templates.preview_survey_question_1_upper_label")),
            isDraft: true,
          },
        ],
        buttonLabel: text(t("templates.next")),
        backButtonLabel: text(t("templates.preview_survey_question_2_back_button_label")),
      },
    ],
    endings: [
      {
        id: "cltyqp5ng000108l9dmxw6nde",
        type: "endScreen",
        headline: text(t("templates.preview_survey_ending_card_headline")),
        subheader: text(t("templates.preview_survey_ending_card_description")),
      },
    ],
    hiddenFields: { enabled: true, fieldIds: [] },
    variables: [],
    displayOption: "displayOnce",
    recontactDays: null,
    displayLimit: null,
    autoClose: null,
    recaptcha: null,
    delay: 0,
    displayPercentage: null,
    workspaceOverwrites: null,
    languages: [],
    triggers: [],
    showLanguageSwitch: false,
    isBackButtonHidden: false,
    isAutoProgressingEnabled: true,
  }) as unknown as TJsWorkspaceStateSurvey;
