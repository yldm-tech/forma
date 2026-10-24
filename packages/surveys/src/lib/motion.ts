/**
 * Whether the respondent asked the operating system to reduce motion. Read at the moment of use rather than once, so changing the setting mid-survey takes effect on the next card.
 */
export const prefersReducedMotion = (): boolean =>
  typeof globalThis.matchMedia === "function" &&
  globalThis.matchMedia("(prefers-reduced-motion: reduce)").matches;

/** How the survey scrolls a field into view: smoothly, unless the respondent asked for less motion. */
export const scrollBehavior = (): ScrollBehavior => (prefersReducedMotion() ? "auto" : "smooth");

/** The stacked card's transition: a slide, or only a short fade when the respondent asked for less motion. */
export const stackedCardTransition = (): string =>
  prefersReducedMotion()
    ? "opacity 150ms ease-in-out"
    : "transform 600ms ease-in-out, opacity 600ms ease-in-out, width 600ms ease-in-out";
