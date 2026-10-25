import { cleanup, render, screen } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test } from "vite-plus/test";
import { initI18n } from "../../../lib/i18n";
import { SurveyEditorTabs } from "./survey-editor-tabs";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(cleanup);

const tabNames = (isCxMode: boolean) => {
  render(
    <I18nextProvider i18n={i18n}>
      <SurveyEditorTabs
        activeId="elements"
        setActiveId={() => {}}
        isStylingTabVisible
        isFollowUpsTabVisible
        isCxMode={isCxMode}
      />
    </I18nextProvider>
  );
  return screen.getAllByRole("button").map((tab) => tab.textContent);
};

describe("SurveyEditorTabs", () => {
  test("the full editor shows every tab", () => {
    expect(tabNames(false)).toEqual(["Questions", "Styling", "Language", "Settings", "Follow-ups"]);
  });

  // The close date, response limit, single-use links and PIN live on Settings: the onboarding editor that publishes a first survey must offer them.
  test("the onboarding (CX) editor keeps Settings and leaves out Language and Follow-ups", () => {
    expect(tabNames(true)).toEqual(["Questions", "Styling", "Settings"]);
  });
});
