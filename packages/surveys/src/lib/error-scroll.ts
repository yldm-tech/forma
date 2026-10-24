/** How much of a failed question must show under its headline for the headline to lead: the error line and the first answer. */
export const MIN_VISIBLE_ANSWERS_PX = 96;

/**
 * Whether a failed question is brought into view by its headline (true) or by its answers (false).
 *
 * The headline says what the question asks, so it leads whenever the error message and the first answer still fit under it in the visible height. Centring the answers instead pushed the headline and its "Required" badge above the card's top on a short screen. Only a header taller than that (long text, an image or a video) gives way to the answers, so the error itself is never out of view.
 */
export const shouldLeadWithHeadline = (headerHeight: number, visibleHeight: number): boolean =>
  headerHeight + MIN_VISIBLE_ANSWERS_PX <= visibleHeight;

const pixels = (value: string): number => {
  const parsed = Number.parseFloat(value);
  return Number.isFinite(parsed) ? parsed : 0;
};

/** The height a scrolled-to element can show in: the card's scrolling area less its scroll padding, or the viewport when the card does not scroll on its own (cardless). */
const visibleHeightAround = (element: HTMLElement): number => {
  const scroller = element.closest<HTMLElement>("[data-survey-scroller]");
  if (!scroller) return element.ownerDocument.defaultView?.innerHeight ?? 0;
  const style = getComputedStyle(scroller);
  return scroller.clientHeight - pixels(style.scrollPaddingTop) - pixels(style.scrollPaddingBottom);
};

/**
 * Scrolls a failed question into view: its headline at the top when the error and the first answer fit under it, otherwise its answers (which start with the error message) at the top.
 */
export const scrollToFailedElement = (form: HTMLElement, behavior: ScrollBehavior): void => {
  const answers = form.querySelector<HTMLElement>("[data-element-input]");
  if (!answers) {
    form.scrollIntoView({ behavior, block: "start" });
    return;
  }
  const headerHeight = answers.getBoundingClientRect().top - form.getBoundingClientRect().top;
  const target = shouldLeadWithHeadline(headerHeight, visibleHeightAround(form)) ? form : answers;
  target.scrollIntoView({ behavior, block: "start" });
};
