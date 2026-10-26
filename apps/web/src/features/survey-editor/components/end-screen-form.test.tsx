import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import type { TSurvey, TSurveyEndScreenCard } from "@forma/types/surveys/types";
import { afterEach, beforeAll, describe, expect, test } from "vite-plus/test";
import { initI18n } from "../../../lib/i18n";
import { EndScreenForm } from "./end-screen-form";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
});

const ending: TSurveyEndScreenCard = {
  id: "end",
  type: "endScreen",
  headline: { default: "Thank you!" },
  subheader: { default: "We appreciate your feedback." },
};

// Renders the form the way the editor does: every update is merged into the card (edit-ending-card spreads it), and `remount` stands in for collapsing and reopening the card, which unmounts the form.
const renderForm = (initial: TSurveyEndScreenCard = ending) => {
  const updates: Partial<TSurveyEndScreenCard>[] = [];
  let card = initial;
  let key = 0;
  const element = () => (
    <I18nextProvider i18n={i18n}>
      <EndScreenForm
        key={key}
        localSurvey={{ languages: [], blocks: [], endings: [card] } as unknown as TSurvey}
        endingCardIndex={0}
        isInvalid={false}
        selectedLanguageCode="default"
        updateSurvey={(input) => {
          updates.push(input as Partial<TSurveyEndScreenCard>);
          card = { ...card, ...(input as Partial<TSurveyEndScreenCard>) };
          view.rerender(element());
        }}
        endingCard={card}
        locale="en-US"
      />
    </I18nextProvider>
  );
  const view = render(element());
  const remount = () => {
    key += 1;
    view.rerender(element());
  };
  return { updates, remount, getCard: () => card };
};

const buttonSwitch = () => screen.getByRole("switch", { name: /Show button/ });

describe("EndScreenForm", () => {
  test("turning the button on leaves its label and link for the author to write, with no link to Forma", () => {
    const { updates } = renderForm();
    fireEvent.click(buttonSwitch());
    expect(updates).toEqual([{ buttonLabel: { default: "" }, buttonLink: "" }]);
  });

  test("a button whose label and link are still empty stays on, with its fields shown, after the card is reopened", () => {
    const { remount } = renderForm();
    fireEvent.click(buttonSwitch());
    remount();
    expect(buttonSwitch().getAttribute("aria-checked")).toBe("true");
    expect(document.getElementById("buttonLink")).not.toBeNull();
    expect(screen.getByText("Button Label")).toBeTruthy();
  });

  test("turning the button off removes its label and link and hides the fields", () => {
    const { remount, getCard } = renderForm({ ...ending, buttonLabel: { default: "" }, buttonLink: "" });
    expect(buttonSwitch().getAttribute("aria-checked")).toBe("true");
    fireEvent.click(buttonSwitch());
    remount();
    expect(getCard().buttonLabel).toBeUndefined();
    expect(getCard().buttonLink).toBeUndefined();
    expect(buttonSwitch().getAttribute("aria-checked")).toBe("false");
    expect(document.getElementById("buttonLink")).toBeNull();
  });

  test("the empty button fields suggest nothing about Forma", () => {
    renderForm({ ...ending, buttonLabel: { default: "" }, buttonLink: "" });
    expect(document.getElementById("buttonLink")?.getAttribute("placeholder")).toBe("https://example.com");
    expect(document.body.innerHTML).not.toContain("forma.yldm.ai");
    expect(document.body.textContent).not.toContain("Create your own survey");
  });
});
