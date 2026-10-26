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
      <output data-testid="perBrowser">{String(localSurvey.isSingleResponsePerBrowserEnabled)}</output>
      <output data-testid="perEmail">{String(localSurvey.isSingleResponsePerEmailEnabled)}</output>
      <output data-testid="publishOn">{localSurvey.publishOn?.toISOString() ?? ""}</output>
      <output data-testid="closeOn">{localSurvey.closeOn?.toISOString() ?? ""}</output>
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

const perBrowserSwitch = () => screen.getByRole("switch", { name: /^One response per browser/ });
const perEmailSwitch = () => screen.getByRole("switch", { name: /^One response per email address/ });

describe("duplicate-submission limits", () => {
  test("one response per browser is off for a link survey, and the switch turns it on and off", () => {
    renderCard(survey());
    expect(perBrowserSwitch().getAttribute("aria-checked")).toBe("false");
    expect(screen.getByText(/Best effort: a private window/)).toBeTruthy();

    fireEvent.click(perBrowserSwitch());
    expect(screen.getByTestId("perBrowser").textContent).toBe("true");
    expect(perBrowserSwitch().getAttribute("aria-checked")).toBe("true");

    fireEvent.click(perBrowserSwitch());
    expect(screen.getByTestId("perBrowser").textContent).toBe("false");
  });

  test("one response per email waits for email verification, then turns on and off", () => {
    renderCard(survey());
    expect(perEmailSwitch().hasAttribute("disabled")).toBe(true);
    expect(screen.getByText(/Turn on "Verify email before submission"/)).toBeTruthy();

    fireEvent.click(screen.getByRole("switch", { name: /^Verify email before submission/ }));
    expect(perEmailSwitch().hasAttribute("disabled")).toBe(false);
    expect(perEmailSwitch().getAttribute("aria-checked")).toBe("false");

    fireEvent.click(perEmailSwitch());
    expect(screen.getByTestId("perEmail").textContent).toBe("true");
    expect(perEmailSwitch().getAttribute("aria-checked")).toBe("true");
    expect(screen.getByText(/refuses a second response from the same address/)).toBeTruthy();

    fireEvent.click(perEmailSwitch());
    expect(screen.getByTestId("perEmail").textContent).toBe("false");
  });

  test("a stored per-email limit shows off while verification is off, and is kept", () => {
    renderCard(survey({ isSingleResponsePerEmailEnabled: true }));
    expect(perEmailSwitch().getAttribute("aria-checked")).toBe("false");
    expect(screen.getByTestId("perEmail").textContent).toBe("true");
  });

  test("are not offered for an app survey", () => {
    renderCard(survey({ type: "app" }));
    expect(screen.queryByRole("switch", { name: /^One response per/ })).toBeNull();
  });
});

describe("publish and close times", () => {
  // Far enough ahead that every time of the day is still to come.
  const publishOn = new Date("2031-05-01T00:00:00.000Z");
  const closeOn = new Date("2031-05-03T00:00:00.000Z");

  const publishTime = () => screen.getByRole("combobox", { name: "Publish time" }) as HTMLSelectElement;
  const closeTime = () => screen.getByRole("combobox", { name: "Close time" }) as HTMLSelectElement;

  test("a scheduled date shows its time, and picking another keeps the day", () => {
    renderCard(survey({ status: "paused", publishOn, closeOn } as Partial<TSurvey>));
    expect(publishTime().value).toBe("0:0");
    expect(publishTime().options).toHaveLength(96);

    fireEvent.change(publishTime(), { target: { value: "18:15" } });
    expect(screen.getByTestId("publishOn").textContent).toBe("2031-05-01T18:15:00.000Z");
    expect(screen.getByText(/^Scheduled for .*6:15\s?PM \(UTC\)$/)).toBeTruthy();

    fireEvent.change(closeTime(), { target: { value: "9:45" } });
    expect(screen.getByTestId("closeOn").textContent).toBe("2031-05-03T09:45:00.000Z");
  });

  test("the close date drops when the publish time moves past it", () => {
    renderCard(
      survey({
        status: "paused",
        publishOn,
        closeOn: new Date("2031-05-01T12:00:00.000Z"),
      } as Partial<TSurvey>)
    );
    // On the publish day, the close time offers only the times after the publish time.
    expect(closeTime().options[0]?.value).toBe("0:15");

    fireEvent.change(publishTime(), { target: { value: "13:0" } });
    expect(screen.getByTestId("publishOn").textContent).toBe("2031-05-01T13:00:00.000Z");
    expect(screen.getByTestId("closeOn").textContent).toBe("");
  });

  test("says the dates open and close at the picked time in the scheduling zone", () => {
    renderCard(survey());
    expect(
      screen.getByText("The survey is published on the selected date and time in the UTC time zone")
    ).toBeTruthy();
    expect(
      screen.getByText("The survey is closed on the selected date and time in the UTC time zone")
    ).toBeTruthy();
  });
});
