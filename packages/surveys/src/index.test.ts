// @vitest-environment happy-dom
import { render } from "preact";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { type SurveyContainerProps } from "@forma/types/forma-surveys";
import { renderSurvey } from "./index";

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
