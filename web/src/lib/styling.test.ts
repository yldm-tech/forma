import { getContrastRatio } from "@forma/types/colors";
import { expect, test } from "vite-plus/test";
import {
  STYLE_DEFAULTS,
  applySuggestedColors,
  formatDimension,
  getSuggestedColors,
  parseDimension,
  themeFormValues,
} from "./styling";

test("the defaults derive from the default brand color", () => {
  expect(STYLE_DEFAULTS.brandColor?.light).toBe("#1e40af");
  expect(STYLE_DEFAULTS.cardArrangement).toEqual({ linkSurveys: "cardless", appSurveys: "simple" });
  expect(STYLE_DEFAULTS.roundness).toBe(8);
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
