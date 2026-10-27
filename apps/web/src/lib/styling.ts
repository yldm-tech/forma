import { ensureReadable, getReadableTextColor, mixColor } from "@forma/types/colors";
import { DEFAULT_CARD_ARRANGEMENT } from "@forma/types/styling";
import type { TWorkspaceStyling } from "@forma/types/workspace";

// forma2's styling defaults and color suggestions (apps/web/lib/styling/constants.ts), the source of the workspace theme form's defaults, its "Suggest colors" action and its reset target.

export const DEFAULT_BRAND_COLOR = "#1e40af";

/** forma2's SURVEY_BG_COLORS: the swatches offered for a link survey's background. */
export const SURVEY_BG_COLORS = [
  "#FFFFFF",
  "#FFF2D8",
  "#EAD7BB",
  "#BCA37F",
  "#113946",
  "#04364A",
  "#176B87",
  "#64CCC5",
  "#DAFFFB",
  "#132043",
  "#1F4172",
  "#F1B4BB",
  "#FDF0F0",
  "#001524",
  "#445D48",
  "#D6CC99",
  "#FDE5D4",
  "#BEADFA",
  "#D0BFFF",
  "#DFCCFB",
] as const;

/** The color fields every suggestion sets, by styling key. */
export type SuggestedColors = Partial<
  Record<Exclude<keyof TWorkspaceStyling, "background">, { light: string }>
> & {
  background: { bg: string; bgType: "color"; brightness: number };
};

/** forma2's getSuggestedColors: a cohesive palette derived from one brand color, with text colors that clear WCAG AA on the surfaces they land on. */
export const getSuggestedColors = (brandColor: string = DEFAULT_BRAND_COLOR): SuggestedColors => {
  const inputBg = mixColor(brandColor, "#ffffff", 0.92);
  const inputBorder = mixColor(brandColor, "#ffffff", 0.6);
  const cardBorder = mixColor(brandColor, "#ffffff", 0.8);
  const pageBg = mixColor(brandColor, "#ffffff", 0.855);
  // A selected option darkens the option background by 5%, the darkest surface the shared text color renders on.
  const optionSelectedBg = mixColor(inputBg, "#000000", 0.05);
  const questionColor = ensureReadable(mixColor(brandColor, "#000000", 0.35), optionSelectedBg);
  return {
    brandColor: { light: brandColor },
    elementHeadlineColor: { light: questionColor },
    elementDescriptionColor: { light: questionColor },
    elementUpperLabelColor: { light: questionColor },
    buttonBgColor: { light: brandColor },
    buttonTextColor: { light: getReadableTextColor(brandColor) },
    inputBgColor: { light: inputBg },
    inputBorderColor: { light: inputBorder },
    inputTextColor: { light: questionColor },
    optionBgColor: { light: inputBg },
    optionLabelColor: { light: questionColor },
    optionBorderColor: { light: inputBorder },
    cardBackgroundColor: { light: "#ffffff" },
    cardBorderColor: { light: cardBorder },
    highlightBorderColor: { light: mixColor(brandColor, "#ffffff", 0.25) },
    progressIndicatorBgColor: { light: brandColor },
    progressTrackBgColor: { light: mixColor(brandColor, "#ffffff", 0.8) },
    background: { bg: pageBg, bgType: "color", brightness: 100 },
  };
};

const colors = getSuggestedColors(DEFAULT_BRAND_COLOR);

/** forma2's STYLE_DEFAULTS: every styling default, colors derived from the default brand color. */
export const STYLE_DEFAULTS: TWorkspaceStyling = {
  allowStyleOverwrite: true,
  brandColor: colors.brandColor,
  inputBorderColor: colors.inputBorderColor,
  cardBackgroundColor: colors.cardBackgroundColor,
  cardBorderColor: colors.cardBorderColor,
  isLogoHidden: false,
  highlightBorderColor: colors.highlightBorderColor,
  isDarkModeEnabled: false,
  roundness: 8,
  // What respondents see when nothing is stored (D8): showing anything else here would change the layout the moment the form is saved.
  cardArrangement: { ...DEFAULT_CARD_ARRANGEMENT },
  linkSurveyCardWidth: "default",
  elementHeadlineColor: colors.elementHeadlineColor,
  elementHeadlineFontSize: 16,
  elementHeadlineFontWeight: 500,
  elementDescriptionColor: colors.elementDescriptionColor,
  elementDescriptionFontSize: 14,
  elementDescriptionFontWeight: 400,
  elementUpperLabelColor: colors.elementUpperLabelColor,
  elementUpperLabelFontSize: 12,
  elementUpperLabelFontWeight: 400,
  inputBgColor: colors.inputBgColor,
  inputTextColor: colors.inputTextColor,
  inputBorderRadius: 8,
  inputHeight: 20,
  inputFontSize: 14,
  inputPaddingX: 8,
  inputPaddingY: 8,
  inputPlaceholderOpacity: 0.5,
  inputShadow: "0 1px 2px 0 rgb(0 0 0 / 0.05)",
  buttonBgColor: colors.buttonBgColor,
  buttonTextColor: colors.buttonTextColor,
  buttonBorderRadius: 8,
  buttonHeight: "auto",
  buttonFontSize: 16,
  buttonFontWeight: 500,
  buttonPaddingX: 12,
  buttonPaddingY: 12,
  optionBgColor: colors.optionBgColor,
  optionLabelColor: colors.optionLabelColor,
  optionBorderColor: colors.optionBorderColor,
  optionBorderRadius: 8,
  optionPaddingX: 16,
  optionPaddingY: 16,
  optionFontSize: 14,
  progressTrackHeight: 8,
  progressTrackBgColor: colors.progressTrackBgColor,
  progressIndicatorBgColor: colors.progressIndicatorBgColor,
};

/** The theme form's starting values, as forma2's ThemeStyling built them: the saved styling's set values over STYLE_DEFAULTS, so styling saved before a field existed shows that field's default. */
export const themeFormValues = (saved: Partial<TWorkspaceStyling> | null | undefined): TWorkspaceStyling => {
  const set = Object.fromEntries(Object.entries(saved ?? {}).filter(([, value]) => value != null));
  return { ...STYLE_DEFAULTS, ...set };
};

/** forma2's "Suggest colors": the form with the palette derived from its brand color applied, and the footer link color cleared so it follows the new palette. */
export const applySuggestedColors = (styling: TWorkspaceStyling): TWorkspaceStyling => {
  const { background, ...suggested } = getSuggestedColors(styling.brandColor?.light ?? DEFAULT_BRAND_COLOR);
  const next: TWorkspaceStyling = { ...styling, background, footerLinkColor: undefined };
  // forma2 set each color's light value, keeping any dark value beside it.
  for (const [key, color] of Object.entries(suggested) as [keyof TWorkspaceStyling, { light: string }][]) {
    const current = styling[key];
    Object.assign(next, { [key]: { ...(typeof current === "object" ? current : {}), light: color.light } });
  }
  return next;
};

/** A dimension field's number and unit: a bare number is pixels, a string carries its unit. */
export const parseDimension = (
  value: number | string | null | undefined
): { amount: number | null; unit: "px" | "%" | "rem" | "em" } => {
  let unit: "px" | "%" | "rem" | "em" = "px";
  if (typeof value === "string") {
    if (value.endsWith("%")) unit = "%";
    else if (value.endsWith("rem")) unit = "rem";
    else if (value.endsWith("em")) unit = "em";
  }
  const parsed = typeof value === "string" ? Number.parseFloat(value) : value;
  return { amount: typeof parsed === "number" && !Number.isNaN(parsed) ? parsed : null, unit };
};

/** The stored form of a dimension: pixels as a number, any other unit as text. */
export const formatDimension = (amount: number, unit: "px" | "%" | "rem" | "em"): number | string =>
  unit === "px" ? amount : `${amount}${unit}`;
