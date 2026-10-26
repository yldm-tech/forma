// What the embed script (/js/embed.js), the survey page inside its frame (apps/web/src/survey-page) and the share modal's snippets (apps/web/src/features/survey-analysis/components/share) agree on. Customer pages carry these names in pasted HTML and the survey page posts these messages to pages the script runs on, so they are a published contract: add to them, never rename one.

/** The query parameter that renders a link survey without its page around it (background, logo, legal footer). The share modal's plain iframe snippet sets it too. */
export const EMBED_PARAM = "embed";

/** The query parameter that lets an embedded survey take the height of its content instead of filling its frame, so the frame can follow it. Only read together with EMBED_PARAM. */
export const AUTO_HEIGHT_PARAM = "autoHeight";

/** The message a survey page in a frame posts to the page around it whenever the height of its content changes: `{ type: RESIZE_MESSAGE_TYPE, height }`, in CSS pixels. */
export const RESIZE_MESSAGE_TYPE = "forma:embed-resize";

export interface ResizeMessage {
  type: typeof RESIZE_MESSAGE_TYPE;
  height: number;
}

/** The message a survey page in one of the embed script's frames posts to the page around it when the respondent presses Escape and nothing in the survey (an open dropdown) took it: `{ type: ESCAPE_MESSAGE_TYPE }`. Focus is inside the survey's frame almost all the time a popup, slider or side tab is open, where the page's own keydown listener never hears the key. */
export const ESCAPE_MESSAGE_TYPE = "forma:embed-escape";

export interface EscapeMessage {
  type: typeof ESCAPE_MESSAGE_TYPE;
}

/** The message the survey runtime posts to the page around it once the last answer has reached the server (packages/surveys, survey.tsx). A bare string, as forma2 sent it. */
export const COMPLETED_MESSAGE = "formaSurveyCompleted";

/** The DOM event the embed script dispatches on the element that holds the embed (it bubbles) when its survey is completed. */
export const COMPLETED_EVENT = "forma:survey-completed";

/** The layouts the embed script renders, one data attribute each. The attribute's value is the survey's link. */
export const EMBED_MODES = ["standard", "popup", "slider", "side-tab"] as const;
export type EmbedMode = (typeof EMBED_MODES)[number];

export const MODE_ATTRIBUTES: Readonly<Record<EmbedMode, string>> = {
  standard: "data-forma-embed",
  popup: "data-forma-popup",
  slider: "data-forma-slider",
  "side-tab": "data-forma-side-tab",
};

/** The options an embed element may carry, besides its mode attribute. */
export const OPTION_ATTRIBUTES = {
  /** The frame's accessible name, and the dialog's. */
  title: "data-forma-title",
  /** Standard: the frame's height in pixels until the survey reports its own. */
  height: "data-forma-height",
  /** Popup, slider and side tab: open on their own this many seconds after the page loads, once per browser session. */
  openDelay: "data-forma-open-delay",
  /** Popup, slider and side tab: close this many seconds after the survey is completed. */
  autoClose: "data-forma-auto-close",
  /** Slider and side tab: the edge of the window they sit on, "right" (the default) or "left". */
  position: "data-forma-position",
  /** Side tab: the text on the tab. */
  label: "data-forma-label",
  /** Side tab: the tab's background colour, any CSS colour. */
  buttonColor: "data-forma-button-color",
  /** The close button's accessible name. */
  closeLabel: "data-forma-close-label",
} as const;

/** The prefix of the session storage keys under which the embed script remembers that a survey already opened on its own (OPTION_ATTRIBUTES.openDelay). */
export const AUTO_OPENED_KEY_PREFIX = "forma-embed:auto-opened:";

/** Whether a message from a survey frame is a resize message with a usable height. */
export const isResizeMessage = (data: unknown): data is ResizeMessage =>
  typeof data === "object" &&
  data !== null &&
  (data as { type?: unknown }).type === RESIZE_MESSAGE_TYPE &&
  typeof (data as { height?: unknown }).height === "number" &&
  Number.isFinite((data as { height: number }).height) &&
  (data as { height: number }).height >= 0;

/** Whether a message from a survey frame is an escape message. */
export const isEscapeMessage = (data: unknown): data is EscapeMessage =>
  typeof data === "object" && data !== null && (data as { type?: unknown }).type === ESCAPE_MESSAGE_TYPE;

/** The address of a survey link in an embed frame: embedded, taking the height of its content. Everything else the link carries (language, hidden fields, prefill) is kept. Null for anything but an http(s) link, so an attribute can never point a frame at a javascript: URL. */
export const embedFrameUrl = (surveyUrl: string, base?: string): URL | null => {
  let url: URL;
  try {
    url = new URL(surveyUrl, base);
  } catch {
    return null;
  }
  if (url.protocol !== "https:" && url.protocol !== "http:") return null;
  url.searchParams.set(EMBED_PARAM, "true");
  url.searchParams.set(AUTO_HEIGHT_PARAM, "true");
  return url;
};
