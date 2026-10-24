// @vitest-environment happy-dom
import { render } from "preact";
import { afterEach, describe, expect, test } from "vite-plus/test";
import { SurveyContainer } from "./survey-container";

afterEach(() => {
  render(null, document.body);
  document.body.innerHTML = "";
});

describe("SurveyContainer", () => {
  // Every DOM change in the survey (the next card, error text, hover fills) was queued for announcement when the dialog sat inside a live region.
  test.each(["none", "dark"] as const)("a modal survey (overlay %s) is not inside a live region", (overlay) => {
    render(
      <SurveyContainer mode="modal" overlay={overlay} surveyName="Quick check">
        <p>Question</p>
      </SurveyContainer>,
      document.body
    );
    const dialog = document.querySelector('[role="dialog"]');
    expect(dialog).not.toBeNull();
    expect(dialog?.closest("[aria-live]")).toBeNull();
  });
});
