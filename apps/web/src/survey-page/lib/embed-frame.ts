import {
  AUTO_HEIGHT_PARAM,
  EMBED_PARAM,
  ESCAPE_MESSAGE_TYPE,
  type EscapeMessage,
  RESIZE_MESSAGE_TYPE,
  type ResizeMessage,
} from "@forma/embed/protocol";

/** How a survey page sits in a frame: embedded (no page around the survey), and whether its content takes its own height (the embed script's frames, /js/embed.js) or fills the frame (the share modal's plain iframe snippet). */
export interface EmbedFrameMode {
  isEmbed: boolean;
  isAutoHeight: boolean;
}

export const readEmbedFrameMode = (search: string): EmbedFrameMode => {
  const params = new URLSearchParams(search);
  const isEmbed = params.get(EMBED_PARAM) === "true";
  return { isEmbed, isAutoHeight: isEmbed && params.get(AUTO_HEIGHT_PARAM) === "true" };
};

/** The box Radix puts around a floating layer (a dropdown question's options) it portals to the end of <body>, outside #root. */
const POPPER_SELECTOR = "[data-radix-popper-content-wrapper]";

/** Room below an open dropdown for the gap to its trigger and its shadow. */
const POPPER_MARGIN = 8;

/**
 * How far down the document an open dropdown needs to reach to show all of its options: below its trigger, at its natural height. Radix keeps a dropdown inside the viewport, which in a frame that follows the survey's height is the frame itself, by placing it above its trigger or by cutting its height to the space left; measuring where it is drawn would therefore never ask for more room than the frame already has. The trigger is the element whose aria-controls names the dropdown; a layer with none (a tooltip) counts from where it is drawn.
 */
const popperBottom = (wrapper: Element): number => {
  const content = wrapper.firstElementChild;
  if (!(content instanceof HTMLElement)) return 0;
  const doc = wrapper.ownerDocument;
  const anchor = content.id
    ? [...doc.querySelectorAll("[aria-controls]")].find(
        (candidate) => candidate.getAttribute("aria-controls") === content.id
      )
    : undefined;
  // scrollHeight is the content's natural height whatever height Radix cut it to; the borders are not part of it.
  const naturalHeight = content.scrollHeight + content.offsetHeight - content.clientHeight;
  const top = anchor ? anchor.getBoundingClientRect().bottom : content.getBoundingClientRect().top;
  return (doc.defaultView?.scrollY ?? 0) + top + naturalHeight + POPPER_MARGIN;
};

/**
 * Posts the height of element to the page around the frame whenever it changes, so that page can size the frame to it (packages/embed). While a dropdown is open, the height is at least what the dropdown needs (popperBottom), so a frame that follows it grows to show the options instead of cutting them off; it shrinks back when the dropdown closes. The target origin is "*": the survey cannot know which site embeds it, and its height tells that site nothing it could not measure by embedding the same link itself. Nothing is posted while element has no height (before the first render). Returns the function that stops it.
 */
export const reportEmbedHeight = (
  element: HTMLElement,
  target: Pick<Window, "postMessage">
): (() => void) => {
  const doc = element.ownerDocument;
  let reported = 0;
  const report = () => {
    const contentHeight = element.getBoundingClientRect().height;
    if (contentHeight === 0) return;
    const poppers = [...doc.querySelectorAll(POPPER_SELECTOR)].map(popperBottom);
    const height = Math.ceil(Math.max(contentHeight, ...poppers));
    if (height === reported) return;
    reported = height;
    const message: ResizeMessage = { type: RESIZE_MESSAGE_TYPE, height };
    target.postMessage(message, "*"); // NOSONAR typescript:S2819 // The embedding page's origin is unknown here, and the height is not a secret.
  };
  const resizeObserver = new ResizeObserver(report);
  resizeObserver.observe(element);
  // Dropdowns are portaled to <body>, outside element: watch them come and go, and follow the size of the open ones (a search that narrows the options).
  const watchedPoppers = new Set<Element>();
  const watchPoppers = () => {
    for (const watched of watchedPoppers) {
      if (watched.isConnected) continue;
      resizeObserver.unobserve(watched);
      watchedPoppers.delete(watched);
    }
    for (const wrapper of doc.querySelectorAll(POPPER_SELECTOR)) {
      const content = wrapper.firstElementChild;
      for (const part of content ? [content, ...content.children] : []) {
        if (watchedPoppers.has(part)) continue;
        watchedPoppers.add(part);
        resizeObserver.observe(part);
      }
    }
    report();
  };
  const mutationObserver = new MutationObserver(watchPoppers);
  mutationObserver.observe(doc.body, { childList: true, subtree: true });
  report();
  return () => {
    resizeObserver.disconnect();
    mutationObserver.disconnect();
  };
};

/**
 * Tells the page around the frame that the respondent pressed Escape, so the embed script can close the popup, slider or side tab the survey is in: focus is inside the survey's frame almost all the time one is open, where the page's own keydown listener never hears the key. An Escape something in the survey already took (an open dropdown closing, which Radix marks with preventDefault) is not passed on, nor one that ends an IME composition. Returns the function that stops it.
 */
export const reportEmbedEscape = (
  win: Pick<Window, "addEventListener" | "removeEventListener">,
  target: Pick<Window, "postMessage">
): (() => void) => {
  const onKeyDown = (event: KeyboardEvent) => {
    if (event.key !== "Escape" || event.defaultPrevented || event.isComposing) return;
    const message: EscapeMessage = { type: ESCAPE_MESSAGE_TYPE };
    target.postMessage(message, "*"); // NOSONAR typescript:S2819 // The embedding page's origin is unknown here, and a key press carries nothing secret.
  };
  win.addEventListener("keydown", onKeyDown);
  return () => win.removeEventListener("keydown", onKeyDown);
};
