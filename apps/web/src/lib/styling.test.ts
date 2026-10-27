import { getContrastRatio } from "@forma/types/colors";
import { DEFAULT_CARD_ARRANGEMENT } from "@forma/types/styling";
import { expect, test } from "vite-plus/test";
import {
  STYLE_DEFAULTS,
  applySuggestedColors,
  formatDimension,
  getSuggestedColors,
  parseDimension,
  themeFormValues,
} from "./styling";
import { STYLE_DEFAULTS as SURVEY_STYLE_DEFAULTS } from "./survey/styling";

test("the defaults derive from the default brand color", () => {
  expect(STYLE_DEFAULTS.brandColor?.light).toBe("#1e40af");
  expect(STYLE_DEFAULTS.roundness).toBe(8);
});

// The respondent runtime (packages/surveys survey.tsx) renders DEFAULT_CARD_ARRANGEMENT when nothing is stored. If the Appearance page or the editor's "Add custom styles" started from anything else, saving either form without touching the arrangement would change the layout respondents see.
test("the admin forms start from the card arrangement respondents see when none is stored", () => {
  expect(DEFAULT_CARD_ARRANGEMENT).toEqual({ linkSurveys: "straight", appSurveys: "straight" });
  expect(STYLE_DEFAULTS.cardArrangement).toEqual(DEFAULT_CARD_ARRANGEMENT);
  expect(SURVEY_STYLE_DEFAULTS.cardArrangement).toEqual(DEFAULT_CARD_ARRANGEMENT);
  expect(themeFormValues({}).cardArrangement).toEqual(DEFAULT_CARD_ARRANGEMENT);
  expect(themeFormValues({ cardArrangement: null }).cardArrangement).toEqual(DEFAULT_CARD_ARRANGEMENT);
});

test("a stored card arrangement wins over the default", () => {
  const stored = { linkSurveys: "cardless", appSurveys: "simple" } as const;
  expect(themeFormValues({ cardArrangement: stored }).cardArrangement).toEqual(stored);
});

test("suggested text colors are readable on the option background", () => {
  const colors = getSuggestedColors("#ffcc00");
  const headline = colors.elementHeadlineColor?.light ?? "";
  const optionBg = colors.optionBgColor?.light ?? "";
  expect(getContrastRatio(headline, optionBg)).toBeGreaterThanOrEqual(4.5);
  expect(colors.background).toMatchObject({ bgType: "color", brightness: 100 });
});

test("saved values override the defaults, unset ones do not", () => {
  const values = themeFormValues({
    allowStyleOverwrite: false,
    roundness: null,
    brandColor: { light: "#000000" },
  });
  expect(values.allowStyleOverwrite).toBe(false);
  expect(values.roundness).toBe(8);
  expect(values.brandColor?.light).toBe("#000000");
});

test("suggesting colors keeps dark values and clears the footer link color", () => {
  const next = applySuggestedColors({
    ...STYLE_DEFAULTS,
    brandColor: { light: "#123456", dark: "#654321" },
    footerLinkColor: { light: "#ff0000" },
  });
  expect(next.brandColor).toEqual({ light: "#123456", dark: "#654321" });
  expect(next.buttonBgColor?.light).toBe("#123456");
  expect(next.footerLinkColor).toBeUndefined();
});

test("dimensions are pixels as numbers and other units as text", () => {
  expect(parseDimension(12)).toEqual({ amount: 12, unit: "px" });
  expect(parseDimension("1.5rem")).toEqual({ amount: 1.5, unit: "rem" });
  expect(parseDimension("auto")).toEqual({ amount: null, unit: "px" });
  expect(formatDimension(4, "px")).toBe(4);
  expect(formatDimension(50, "%")).toBe("50%");
});
