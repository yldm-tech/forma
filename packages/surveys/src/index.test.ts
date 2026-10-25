// @vitest-environment happy-dom
import { render } from "preact";
import { beforeEach, describe, expect, test, vi } from "vite-plus/test";
import { type SurveyContainerProps } from "@forma/types/forma-surveys";
import { loadSurveyLanguage } from "@/lib/i18n.config";
import { LANGUAGE_WAIT_MS, renderSurvey } from "./index";

// The renderer itself is out of scope here: these tests are about the container element the modal
// branch manages, so preact's render is a spy and the survey tree is never mounted.
vi.mock("preact", async (importOriginal) => {
  const actual = await importOriginal<typeof import("preact")>();
  return { ...actual, render: vi.fn() };
});

vi.mock("@/components/general/render-survey", () => ({ RenderSurvey: () => null }));
vi.mock("@/components/i18n/provider", () => ({ I18nProvider: () => null }));
vi.mock("@/lib/styles", () => ({
  addStylesToDom: vi.fn(),
  addCustomThemeToDom: vi.fn(),
  setStyleNonce: vi.fn(),
}));
// English, which the bundle carries, unless a test says the language still has to be fetched.
vi.mock("@/lib/i18n.config", () => ({ loadSurveyLanguage: vi.fn(() => undefined) }));

const MODAL_CONTAINER_ID = "forma-modal-container";

const buildProps = (surveyId: string): SurveyContainerProps =>
  ({
    survey: { id: surveyId, type: "app", languages: [] },
    styling: {},
    isBrandingEnabled: false,
    languageCode: "default",
  }) as unknown as SurveyContainerProps;

describe("renderSurvey in modal mode", () => {
  beforeEach(() => {
    document.body.innerHTML = "";
    vi.mocked(render).mockClear();
  });

  test("appends a single modal container", () => {
    renderSurvey(buildProps("survey-1"));

    const containers = document.querySelectorAll(`#${MODAL_CONTAINER_ID}`);
    expect(containers).toHaveLength(1);
    expect(vi.mocked(render).mock.calls.at(-1)?.[1]).toBe(containers[0]);
  });

  test("replaces the existing container when a second survey renders over a live one", () => {
    renderSurvey(buildProps("survey-1"));
    const firstContainer = document.getElementById(MODAL_CONTAINER_ID);

    renderSurvey(buildProps("survey-2"));

    // Two elements with the same id would make js-core's removeWidgetContainer (getElementById)
    // resolve the first survey's container and tear that one down instead of the dismissed one.
    const containers = document.querySelectorAll(`#${MODAL_CONTAINER_ID}`);
    expect(containers).toHaveLength(1);
    expect(containers[0]).not.toBe(firstContainer);
    expect(firstContainer?.isConnected).toBe(false);
    // The first survey's tree is unmounted rather than left running in a detached node.
    expect(vi.mocked(render)).toHaveBeenCalledWith(null, firstContainer);
  });
});

describe("renderSurvey with a language that is still being fetched", () => {
  const INLINE_ID = "forma-survey-container";

  const inlineProps = (surveyId: string, languageCode: string): SurveyContainerProps =>
    ({
      survey: { id: surveyId, type: "link", languages: [] },
      styling: {},
      isBrandingEnabled: false,
      languageCode,
      mode: "inline",
      containerId: INLINE_ID,
      appUrl: "https://forma.example",
    }) as unknown as SurveyContainerProps;

  // A language whose fetch the test finishes by calling the returned function.
  const fetchingLanguage = () => {
    let finish = () => {};
    const loaded = new Promise<void>((resolve) => {
      finish = resolve;
    });
    vi.mocked(loadSurveyLanguage).mockReturnValueOnce(loaded);
    return async () => {
      finish();
      await loaded;
    };
  };

  beforeEach(() => {
    document.body.innerHTML = `<div id="${INLINE_ID}"></div>`;
    vi.mocked(render).mockClear();
    vi.mocked(loadSurveyLanguage).mockClear();
  });

  test("renders only once the language has arrived, so the first card is not drawn in English", async () => {
    const finish = fetchingLanguage();

    renderSurvey(inlineProps("survey-1", "de-DE"));
    expect(loadSurveyLanguage).toHaveBeenCalledWith("de-DE", "https://forma.example");
    expect(render).not.toHaveBeenCalled();

    await finish();
    expect(render).toHaveBeenCalledTimes(1);
    expect(vi.mocked(render).mock.calls[0]?.[1]).toBe(document.getElementById(INLINE_ID));
  });

  test("a later call for the same container wins over one still waiting", async () => {
    const finish = fetchingLanguage();

    renderSurvey(inlineProps("survey-1", "de-DE"));
    // The respondent's language is switched back to English, which is loaded: this call renders at once.
    renderSurvey(inlineProps("survey-1", "en-US"));
    expect(render).toHaveBeenCalledTimes(1);

    await finish();
    // The German call's props are older than what is on screen, so it must not render over them.
    expect(render).toHaveBeenCalledTimes(1);
  });

  test("renders after LANGUAGE_WAIT_MS when the language has not arrived, and not again when it does", async () => {
    vi.useFakeTimers();
    try {
      const finish = fetchingLanguage();

      renderSurvey(inlineProps("survey-1", "de-DE"));
      await vi.advanceTimersByTimeAsync(LANGUAGE_WAIT_MS - 1);
      expect(render).not.toHaveBeenCalled();
      await vi.advanceTimersByTimeAsync(1);
      expect(render).toHaveBeenCalledTimes(1);

      // The survey on screen switches language in place (react-i18next re-renders on languageChanged).
      await finish();
      expect(render).toHaveBeenCalledTimes(1);
    } finally {
      vi.useRealTimers();
    }
  });

  test("does not render into a container the host removed while the language loaded", async () => {
    const finish = fetchingLanguage();

    renderSurvey(inlineProps("survey-1", "de-DE"));
    document.getElementById(INLINE_ID)?.remove();

    await finish();
    expect(render).not.toHaveBeenCalled();
  });
});
