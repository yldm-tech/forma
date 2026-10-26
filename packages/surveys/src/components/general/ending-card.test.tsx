import { cleanup, render, screen } from "@testing-library/preact";
import { afterEach, describe, expect, test } from "vite-plus/test";
import type { TJsWorkspaceStateSurvey } from "@forma/types/js";
import type { TSurveyEndScreenCard } from "@forma/types/surveys/types";
import { EndingCard } from "./ending-card";

afterEach(() => {
  cleanup();
  document.body.innerHTML = "";
});

const ending: TSurveyEndScreenCard = {
  id: "end",
  type: "endScreen",
  headline: { default: "Thank you for your order!" },
};

const renderEnding = (endingCard: TSurveyEndScreenCard, languageCode = "default") =>
  render(
    <EndingCard
      survey={{ type: "link" } as TJsWorkspaceStateSurvey}
      endingCard={endingCard}
      isRedirectDisabled
      isResponseSendingFinished
      autoFocusEnabled={false}
      isCurrent
      languageCode={languageCode}
      responseData={{}}
      variablesData={{}}
      isPreviewMode
      fullSizeCards={false}
    />
  );

describe("EndingCard", () => {
  test("an ending without a button shows none", () => {
    renderEnding(ending);
    expect(screen.queryByRole("button")).toBeNull();
  });

  // The editor writes an empty label and link when the author turns the button on; until a label is written the preview must not invent a "Next" button.
  test("an ending whose button label is still empty shows no button", () => {
    renderEnding({ ...ending, buttonLabel: { default: "" }, buttonLink: "" });
    expect(screen.queryByRole("button")).toBeNull();
  });

  test("an ending with a button label shows it", () => {
    renderEnding({
      ...ending,
      buttonLabel: { default: "Track my order" },
      buttonLink: "https://example.com",
    });
    expect(screen.getByRole("button", { name: "Track my order" })).toBeTruthy();
  });

  test("a label not yet translated falls back to the default language", () => {
    renderEnding(
      { ...ending, buttonLabel: { default: "Track my order", de: "" }, buttonLink: "https://example.com" },
      "de"
    );
    expect(screen.getByRole("button", { name: "Track my order" })).toBeTruthy();
  });
});
