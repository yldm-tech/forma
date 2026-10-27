import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../lib/i18n";
import { validateSurveyPin } from "../lib/api";
import type { TSurveyProps } from "../lib/state";
import { BACKGROUND_WAIT_MS, PinScreen } from "./pin-screen";

vi.mock("../lib/api", () => ({
  validateSurveyPin: vi.fn(),
  resumeSurveyPin: vi.fn(),
}));

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
  sessionStorage.clear();
  vi.useRealTimers();
  vi.mocked(validateSurveyPin).mockReset();
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

  const darkCardless: TSurveyProps["styling"] = {
    cardArrangement: { linkSurveys: "cardless", appSurveys: "straight" },
    background: { bg: "#111827", bgType: "color", brightness: 100 },
    elementHeadlineColor: { light: "#f9fafb" },
    inputBgColor: { light: "#1f2937" },
    inputTextColor: { light: "#f9fafb" },
    inputBorderColor: { light: "#4b5563" },
  };

  const digitBoxes = () => screen.getAllByRole<HTMLInputElement>("textbox");

  // The admin Input is transparent with slate-800 digits and the message is red-700: on a dark cardless background both were close to invisible.
  test("on a dark cardless background the boxes take the theme's input colors and the message the headline color", async () => {
    vi.mocked(validateSurveyPin).mockRejectedValue(new Error("network"));
    renderGate(darkCardless);
    await screen.findByRole("heading", { name: i18n.t("s.enter_pin") });

    for (const box of digitBoxes()) {
      expect(box.style.backgroundColor).toBe("rgb(31, 41, 55)");
      expect(box.style.color).toBe("rgb(249, 250, 251)");
      expect(box.style.borderColor).toBe("rgb(75, 85, 99)");
    }

    digitBoxes().forEach((box, index) => fireEvent.change(box, { target: { value: String(index + 1) } }));
    const status = screen.getByRole("status");
    await waitFor(() =>
      expect(status.textContent).toBe(i18n.t("common.something_went_wrong_please_try_again"))
    );
    expect(status.style.color).toBe("rgb(249, 250, 251)");
    // A refused PIN still marks the boxes red, which the theme's border color must not cover.
    for (const box of digitBoxes()) expect(box.style.borderColor).toBe("");
  });

  test("a cardless theme without input colors still puts the digits on an opaque box", async () => {
    renderGate({
      cardArrangement: { linkSurveys: "cardless", appSurveys: "straight" },
      background: { bg: "#111827", bgType: "color", brightness: 100 },
      elementHeadlineColor: { light: "#f9fafb" },
    });
    await screen.findByRole("heading", { name: i18n.t("s.enter_pin") });
    for (const box of digitBoxes()) {
      expect(box.style.backgroundColor).toBe("rgb(255, 255, 255)");
      expect(box.style.color).toBe("rgb(15, 23, 42)");
    }
  });

  const imageCardless: TSurveyProps["styling"] = {
    ...darkCardless,
    background: { bg: "https://forma.example/storage/night.jpg", bgType: "upload", brightness: 100 },
  };
  const gateOf = (element: HTMLElement) => element.closest<HTMLElement>("[data-pin-gate]");

  // Without the runtime's loading overlay the theme's colors, chosen for the image, would sit on the light placeholder until the image arrives.
  test("on a cardless image background the gate waits for the image before it shows in the theme's colors", async () => {
    const { container } = renderGate(imageCardless);
    const prompt = await screen.findByText(i18n.t("s.enter_pin"));
    expect(gateOf(prompt)?.style.visibility).toBe("hidden");
    // Out of the accessibility tree too until it shows.
    expect(screen.queryByRole("heading", { name: i18n.t("s.enter_pin") })).toBeNull();
    expect(prompt.style.color).toBe("");

    const image = container.querySelector("img[src='https://forma.example/storage/night.jpg']");
    expect(image).not.toBeNull();
    act(() => {
      fireEvent.load(image as HTMLImageElement);
    });
    await waitFor(() => expect(gateOf(prompt)?.style.visibility).toBe(""));
    expect(prompt.style.color).toBe("rgb(249, 250, 251)");
    expect(digitBoxes()[0]?.style.color).toBe("rgb(249, 250, 251)");
  });

  test("a background that never loads does not hold the gate back, and it shows in readable default colors", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    renderGate(imageCardless);
    const prompt = await screen.findByText(i18n.t("s.enter_pin"));
    expect(gateOf(prompt)?.style.visibility).toBe("hidden");
    // Out of the accessibility tree too until it shows.
    expect(screen.queryByRole("heading", { name: i18n.t("s.enter_pin") })).toBeNull();

    act(() => {
      vi.advanceTimersByTime(BACKGROUND_WAIT_MS);
    });
    expect(gateOf(prompt)?.style.visibility).toBe("");
    expect(prompt.style.color).toBe("");
    expect(digitBoxes()[0]?.style.color).toBe("");
  });

  test("a card keeps the gate visible at once: the card is its own surface", async () => {
    renderGate({ background: imageCardless.background });
    const prompt = await screen.findByRole("heading", { name: i18n.t("s.enter_pin") });
    expect(gateOf(prompt)?.style.visibility).toBe("");
  });
});
