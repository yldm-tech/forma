/**
 * The survey's title the server paints over a survey page before either bundle has run (apps/server/internal/linksurvey/firstpaint.go), so a respondent on a slow phone sees what they opened instead of a blank page. It sits outside #root, above the page's own loading overlay; the page takes it away: faded out once the first card is ready, as the overlay fades, or removed at once when the page shows any other screen.
 */

/** The element's id (firstPaintElementID in firstpaint.go). */
export const FIRST_PAINT_ID = "forma-first-paint";

/** The class that fades it out (firstPaintStyle in firstpaint.go). */
export const FIRST_PAINT_DONE_CLASS = "forma-first-paint-done";

/** Whether the server painted the title on this page. */
export const hasFirstPaint = (doc: Document = document): boolean =>
  doc.getElementById(FIRST_PAINT_ID) !== null;

/** Takes the title away at once. */
export const removeFirstPaint = (doc: Document = document): void => {
  doc.getElementById(FIRST_PAINT_ID)?.remove();
};

/** Fades the title out over `fadeMs`, then removes it. The timer covers a fade that fires no `transitionend`, as under prefers-reduced-motion. */
export const fadeOutFirstPaint = (fadeMs: number, doc: Document = document): void => {
  const element = doc.getElementById(FIRST_PAINT_ID);
  if (!element) return;
  const remove = () => {
    element.remove();
  };
  element.addEventListener("transitionend", remove, { once: true });
  setTimeout(remove, fadeMs + 50);
  element.classList.add(FIRST_PAINT_DONE_CLASS);
};
