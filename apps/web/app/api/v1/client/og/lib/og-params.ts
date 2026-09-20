import { ZColor } from "@forma/types/common";
import { hexToRGBA } from "@/lib/utils/colors";

/**
 * The OG image is rendered from an unauthenticated query string, and every distinct `name` is a cache miss that costs a font-shaping and rasterisation pass. The cap is well above any survey name that fits the 800x400 card, so it only ever truncates input that was never going to render anyway.
 */
export const OG_NAME_MAX_LENGTH = 120;

/** Opacity of the card background, previously expressed as the "BF" alpha byte appended to the hex. */
const OG_BACKGROUND_OPACITY = 0.75;

const OG_DEFAULT_BRAND_COLOR = "#0000BF";
const OG_DEFAULT_BACKGROUND_COLOR = "rgba(0, 0, 191, 0.75)";

export type TOgParams = {
  name: string;
  brandColor: string;
  backgroundColor: string;
};

/**
 * Bounds what the OG renderer is handed: a length-capped name and a colour that is a hex literal `ZColor` accepts, never arbitrary caller text.
 *
 * The background used to be built as `brandColor + "BF"`, which is only meaningful for a 6-digit hex — `#abc` became a different colour, `#aabbccdd` became an invalid 10-digit literal. Going through `hexToRGBA` makes every form `ZColor` accepts render the colour that was asked for.
 */
export const parseOgParams = (searchParams: URLSearchParams): TOgParams => {
  const name = (searchParams.get("name") ?? "").slice(0, OG_NAME_MAX_LENGTH);

  const requestedBrandColor = searchParams.get("brandColor");
  const brandColor =
    requestedBrandColor && ZColor.safeParse(requestedBrandColor).success
      ? requestedBrandColor
      : OG_DEFAULT_BRAND_COLOR;

  return {
    name,
    brandColor,
    // Both branches above are a valid hex literal, so the fallback is unreachable; it is here because `hexToRGBA` is typed as optional for the empty-string case.
    backgroundColor: hexToRGBA(brandColor, OG_BACKGROUND_OPACITY) || OG_DEFAULT_BACKGROUND_COLOR,
  };
};
