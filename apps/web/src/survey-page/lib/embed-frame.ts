import {
  AUTO_HEIGHT_PARAM,
  EMBED_PARAM,
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

/**
 * Posts the height of element to the page around the frame whenever it changes, so that page can size the frame to it (packages/embed). The target origin is "*": the survey cannot know which site embeds it, and its height tells that site nothing it could not measure by embedding the same link itself. Nothing is posted while the element has no height (before the first render). Returns the function that stops it.
 */
export const reportEmbedHeight = (
  element: HTMLElement,
  target: Pick<Window, "postMessage">
): (() => void) => {
  let reported = 0;
  const report = () => {
    const height = Math.ceil(element.getBoundingClientRect().height);
    if (height === 0 || height === reported) return;
    reported = height;
    const message: ResizeMessage = { type: RESIZE_MESSAGE_TYPE, height };
    target.postMessage(message, "*"); // NOSONAR typescript:S2819 // The embedding page's origin is unknown here, and the height is not a secret.
  };
  const observer = new ResizeObserver(report);
  observer.observe(element);
  report();
  return () => observer.disconnect();
};
