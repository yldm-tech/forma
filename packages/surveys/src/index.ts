import { h, render } from "preact";
import { SurveyContainerProps } from "@forma/types/forma-surveys";
import { RenderSurvey } from "@/components/general/render-survey";
import { I18nProvider } from "@/components/i18n/provider";
import { FILE_PICK_EVENT } from "@/lib/constants";
import { getI18nLanguage } from "@/lib/i18n-utils";
import { addCustomThemeToDom, addStylesToDom, setStyleNonce } from "@/lib/styles";

// Kept in sync with CONTAINER_ID in packages/js-core/src/lib/common/constants.ts, which is what
// removes this element again; the two packages ship separately, so the string is declared in each.
const MODAL_CONTAINER_ID = "forma-modal-container";

export const renderSurveyInline = (props: SurveyContainerProps) => {
  const inlineProps: SurveyContainerProps = {
    ...props,
    mode: "inline",
  };

  renderSurvey(inlineProps);
};

export const renderSurvey = (props: SurveyContainerProps) => {
  // render SurveyNew
  // if survey type is link, we don't pass the placement, overlay, clickOutside, onClose

  const { mode, containerId, languageCode } = props;

  addStylesToDom();
  addCustomThemeToDom({ styling: props.styling });

  const language = getI18nLanguage(languageCode, props.survey.languages);

  // SDK clients may request a legacy code (e.g. "hi") that is no longer a survey content key after
  // canonicalization (content is keyed "hi-IN"). Render the survey under the resolved canonical code so
  // content lookups — and recall parsing, which indexes content directly — don't hit `undefined`. The
  // "default" sentinel is preserved (content stores its value under the "default" key).
  const surveyLanguageCode = languageCode === "default" ? languageCode : language;

  if (mode === "inline") {
    if (!containerId) {
      throw new Error("renderSurvey: containerId is required for inline mode");
    }

    const element = document.getElementById(containerId);
    if (!element) {
      throw new Error(`renderSurvey: Element with id ${containerId} not found.`);
    }

    // if survey type is link, we don't pass the placement, overlay, clickOutside, onClose
    if (props.survey.type === "link") {
      const { placement, overlay, onClose, clickOutside, ...surveyInlineProps } = props;

      render(
        h(
          I18nProvider,
          { language },
          h(RenderSurvey, {
            ...surveyInlineProps,
            languageCode: surveyLanguageCode,
          })
        ),
        element
      );
    } else {
      // For non-link surveys, pass placement through so it can be used in StackedCard
      const { overlay, onClose, clickOutside, ...surveyInlineProps } = props;

      render(
        h(
          I18nProvider,
          { language },
          h(RenderSurvey, {
            ...surveyInlineProps,
            languageCode: surveyLanguageCode,
          })
        ),
        element
      );
    }
  } else {
    // A second survey can be rendered while a first one is still on screen (js-core's TimeoutStack
    // releases `isSurveyRunning` early — see packages/js-core/src/lib/survey/widget.ts), and appending
    // would then leave two elements carrying this id. `removeWidgetContainer` resolves the first, so
    // dismissing the second survey would tear down the first one's card and leave the second's preact
    // tree — timers, beforeunload handler, response queue — mounted in a detached node. Unmount and
    // drop any existing container first, so at most one ever exists.
    const existingModalContainer = document.getElementById(MODAL_CONTAINER_ID);
    if (existingModalContainer) {
      render(null, existingModalContainer);
      existingModalContainer.remove();
    }

    const modalContainer = document.createElement("div");
    modalContainer.id = MODAL_CONTAINER_ID;
    document.body.appendChild(modalContainer);

    render(
      h(
        I18nProvider,
        { language },
        h(RenderSurvey, {
          ...props,
          languageCode: surveyLanguageCode,
        })
      ),
      modalContainer
    );
  }
};

export const renderSurveyModal = renderSurvey;

export const onFilePick = (files: { name: string; type: string; base64: string }[]) => {
  const fileUploadEvent = new CustomEvent(FILE_PICK_EVENT, { detail: files });
  globalThis.dispatchEvent(fileUploadEvent);
};

// Initialize the global formaSurveys object if it doesn't exist
if (globalThis.window !== undefined) {
  (globalThis.window as any).formaSurveys = {
    renderSurveyInline,
    renderSurveyModal,
    renderSurvey,
    onFilePick,
    setNonce: setStyleNonce,
  } as typeof globalThis.window.formaSurveys;
}
