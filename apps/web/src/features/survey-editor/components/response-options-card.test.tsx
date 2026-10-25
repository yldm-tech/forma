import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { useState } from "react";
import { I18nextProvider } from "react-i18next";
import type { TSurvey } from "@forma/types/surveys/types";
import { afterEach, beforeAll, describe, expect, test } from "vite-plus/test";
import { initI18n } from "../../../lib/i18n";
import { ResponseOptionsCard } from "./response-options-card";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
});

const survey = (overrides: Partial<TSurvey> = {}): TSurvey =>
  ({
    id: "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0e",
    name: "Feedback",
    type: "link",
    status: "draft",
    autoComplete: null,
    publishOn: null,
    closeOn: null,
    pin: null,
    surveyClosedMessage: null,
    recaptcha: null,
    isVerifyEmailEnabled: false,
    isBackButtonHidden: false,
    isAutoProgressingEnabled: false,
    isCaptureIpEnabled: false,
    isAnonymizeResponsesEnabled: false,
    isProgressSavingEnabled: true,
    ...overrides,
  }) as unknown as TSurvey;

/** The card with the survey it edits held in state; the stored setting is printed beside it. */
const Harness = ({ initial }: { initial: TSurvey }) => {
  const [localSurvey, setLocalSurvey] = useState(initial);
  return (
    <>
      <ResponseOptionsCard
        localSurvey={localSurvey}
        setLocalSurvey={setLocalSurvey}
        finishedResponseCount={0}
        isSpamProtectionAllowed={false}
        surveySchedulingConfig={{ timeZone: "UTC", localHour: 0, localMinute: 0 }}
        locale="en-US"
      />
      <output data-testid="stored">{String(localSurvey.isProgressSavingEnabled)}</output>
    </>
  );
};

const renderCard = (initial: TSurvey) =>
  render(
    <I18nextProvider i18n={i18n}>
      <Harness initial={initial} />
    </I18nextProvider>
  );

const stored = () => screen.getByTestId("stored").textContent;

const progressSwitch = () => screen.getByRole("switch", { name: /^Save respondents' progress/ });

describe("saving respondents' progress", () => {
  test("is on for a link survey, and the switch turns it off and on again", () => {
    renderCard(survey());
    expect(progressSwitch().getAttribute("aria-checked")).toBe("true");

    fireEvent.click(progressSwitch());
    expect(stored()).toBe("false");
    expect(progressSwitch().getAttribute("aria-checked")).toBe("false");

    fireEvent.click(progressSwitch());
    expect(stored()).toBe("true");
  });

  test("a survey loaded without the setting shows it on", () => {
    renderCard(survey({ isProgressSavingEnabled: undefined }));
    expect(progressSwitch().getAttribute("aria-checked")).toBe("true");
    fireEvent.click(progressSwitch());
    expect(stored()).toBe("false");
  });

  test("is not offered for an app survey, which keeps no progress", () => {
    renderCard(survey({ type: "app" }));
    expect(screen.queryByRole("switch", { name: /^Save respondents' progress/ })).toBeNull();
  });
});
