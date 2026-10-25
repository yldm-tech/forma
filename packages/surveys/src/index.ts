import { h, render } from "preact";
import { SurveyContainerProps } from "@forma/types/forma-surveys";
import { RenderSurvey } from "@/components/general/render-survey";
import { I18nProvider } from "@/components/i18n/provider";
import { FILE_PICK_EVENT } from "@/lib/constants";
import { getI18nLanguage } from "@/lib/i18n-utils";
import { loadSurveyLanguage } from "@/lib/i18n.config";
import { addCustomThemeToDom, addStylesToDom, setStyleNonce } from "@/lib/styles";

// Kept in sync with CONTAINER_ID in packages/js-core/src/lib/common/constants.ts, which is what
// removes this element again; the two packages ship separately, so the string is declared in each.
const MODAL_CONTAINER_ID = "forma-modal-container";

// The latest renderSurvey call for each container (its id, or the modal container's), by a number that grows with every call that has to wait for its language.
const pendingRenders = new Map<string, number>();
let renderCount = 0;

/**
 * How long renderSurvey waits for a language that is being fetched. A survey must appear even when the locale file is slow or never answers (js-core reports it shown as soon as it asks for it): past this it renders in the language i18next is on, English at first, and the language switches in place when its file arrives.
 */
export const LANGUAGE_WAIT_MS = 2000;

/**
 * Renders once the translations `language` uses are in memory, so the first card is drawn in the respondent's language rather than in English and then again; or after LANGUAGE_WAIT_MS, whichever comes first. A language already loaded (English always is) renders at once, as every call did before the other languages were fetched on demand. A call that waits renders only if no later call for the same container came in meanwhile (that one carries newer props, and may already have rendered) and the container is still in the document.
 */
const renderWhenLanguageReady = (
  containerKey: string,
  language: string,
  appUrl: string | undefined,
  container: HTMLElement,
  renderNow: () => void
) => {
  const loading = loadSurveyLanguage(language, appUrl);
  if (!loading) {
    pendingRenders.delete(containerKey);
    renderNow();
    return;
  }
  const call = ++renderCount;
  pendingRenders.set(containerKey, call);
  // Runs twice, when the language arrives and at the deadline; the first renders, the second finds the call no longer pending.
  const renderIfLatest = () => {
    if (pendingRenders.get(containerKey) !== call) return;
    pendingRenders.delete(containerKey);
    clearTimeout(deadline);
    // The host may have removed the container while the language loaded (a dismissed modal, an unmounted page); a survey rendered into a detached node would keep its timers and listeners running unseen.
    if (container.isConnected) renderNow();
  };
  const deadline = setTimeout(renderIfLatest, LANGUAGE_WAIT_MS);
  void loading.then(renderIfLatest);
};

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

      renderWhenLanguageReady(containerId, language, props.appUrl, element, () =>
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
        )
      );
    } else {
      // For non-link surveys, pass placement through so it can be used in StackedCard
      const { overlay, onClose, clickOutside, ...surveyInlineProps } = props;

      renderWhenLanguageReady(containerId, language, props.appUrl, element, () =>
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
        )
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

    renderWhenLanguageReady(MODAL_CONTAINER_ID, language, props.appUrl, modalContainer, () =>
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
      )
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
