import type { TFunction } from "i18next";
import { describe, expect, test } from "vite-plus/test";
import { getDefaultEndingCard, getDefaultWelcomeCard, hiddenFieldsDefault } from "./survey-builder";

const mockT = ((props: string | { key: string }): string =>
  typeof props === "string" ? props : props.key) as unknown as TFunction;

describe("Survey Builder", () => {
  describe("Helper Functions", () => {
    test("getDefaultWelcomeCard returns expected welcome card", () => {
      const welcomeCard = getDefaultWelcomeCard(mockT);
      expect(welcomeCard).toMatchObject({
        enabled: false,
        headline: { default: "templates.default_welcome_card_headline" },
        timeToFinish: false,
        showResponseCount: false,
      });
      // Check that the welcome card is properly structured
      expect(welcomeCard).toHaveProperty("enabled");
      expect(welcomeCard).toHaveProperty("headline");
      expect(welcomeCard).toHaveProperty("showResponseCount");
      expect(welcomeCard).toHaveProperty("timeToFinish");
    });

    test("getDefaultEndingCard returns expected ending card", () => {
      const endingCard = getDefaultEndingCard([], mockT);
      expect(endingCard).toMatchObject({
        type: "endScreen",
        headline: { default: "templates.default_ending_card_headline" },
        subheader: { default: "templates.default_ending_card_subheader" },
      });
      expect(endingCard.id).toBeDefined();
      // Respondents get no button to Forma on a new ending card; attribution is the dismissible "Powered by" badge.
      expect(endingCard).not.toHaveProperty("buttonLabel");
      expect(endingCard).not.toHaveProperty("buttonLink");
    });

    test("hiddenFieldsDefault has expected structure", () => {
      expect(hiddenFieldsDefault).toMatchObject({
        enabled: true,
        fieldIds: [],
      });
    });
  });
});
