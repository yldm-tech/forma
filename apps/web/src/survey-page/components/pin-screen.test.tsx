import { cleanup, render, screen } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import type { TSurveyProps } from "../lib/state";
import { PinScreen } from "./pin-screen";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
  sessionStorage.clear();
});

const props = (styling: TSurveyProps["styling"]): TSurveyProps => ({
  survey: null,
  surveyId: "0190a000-0000-7000-8000-0000000000aa",
  workspace: {
    styling: { allowStyleOverwrite: true },
    logo: { url: "https://forma.example/storage/logo.png" },
    linkSurveyBranding: true,
    customHeadScripts: null,
  },
  styling,
  publicDomain: "https://forma.example",
  responseCount: null,
  languageCode: "default",
  locale: "en-US",
  isEmbed: false,
  isPreview: false,
  canReadUserIdFromUrl: false,
  isSpamProtectionEnabled: false,
  privacyUrl: "https://forma.example/privacy",
  isFormaCloud: false,
});

const renderGate = (styling: TSurveyProps["styling"]) =>
  render(
    <I18nextProvider i18n={i18n}>
      <PinScreen {...props(styling)} />
    </I18nextProvider>
  );

describe("PinScreen", () => {
  // The gate used to be plain black text on a white page; it is the first screen of the survey, so it carries the survey's page around it.
  test("asks for the PIN on the survey's own page: background, logo and legal footer", async () => {
    const { container } = renderGate({ background: { bg: "#123456", bgType: "color", brightness: 100 } });

    const prompt = await screen.findByRole("heading", { name: i18n.t("s.enter_pin") });
    expect(screen.getByRole("main").contains(prompt)).toBe(true);
    expect(screen.getByRole("img", { name: i18n.t("common.company_logo") })).toBeTruthy();
    expect(screen.getByRole("link", { name: i18n.t("common.privacy") })).toBeTruthy();
    const backgrounds = [...container.querySelectorAll<HTMLElement>("[style]")].map(
      (element) => element.style.backgroundColor
    );
    expect(backgrounds).toContain("rgb(18, 52, 86)");
    // The overlay that waits for the runtime's first card would cover the gate for good: the runtime only loads behind it.
    expect(container.querySelector(".z-5000")).toBeNull();
    expect(screen.getAllByRole("textbox")).toHaveLength(4);
  });

  test("puts the prompt on the survey's card, the default arrangement when none is stored", async () => {
    renderGate({});
    const prompt = await screen.findByRole("heading", { name: i18n.t("s.enter_pin") });
    // StackedCardsContainer's "straight" arrangement: the front card with two cards peeking out behind it.
    const card = prompt.closest(".rounded-xl");
    expect(card).not.toBeNull();
    expect(card?.parentElement?.querySelectorAll(":scope > .rounded-xl")).toHaveLength(3);
  });

  test("a cardless survey keeps the prompt on the background, in the theme's headline color", async () => {
    renderGate({
      cardArrangement: { linkSurveys: "cardless", appSurveys: "straight" },
      elementHeadlineColor: { light: "#fafafa" },
    });
    const prompt = await screen.findByRole("heading", { name: i18n.t("s.enter_pin") });
    expect(prompt.closest(".rounded-xl")).toBeNull();
    expect(prompt.style.color).toBe("rgb(250, 250, 250)");
  });
});
