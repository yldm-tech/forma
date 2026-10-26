import {
  AUTO_OPENED_KEY_PREFIX,
  COMPLETED_EVENT,
  COMPLETED_MESSAGE,
  type EmbedMode,
  MODE_ATTRIBUTES,
  OPTION_ATTRIBUTES,
  embedFrameUrl,
  isEscapeMessage,
  isResizeMessage,
} from "./protocol";

/** The options of one embed, read from its element's data attributes (OPTION_ATTRIBUTES) or passed to open(). */
export interface EmbedOptions {
  title?: string;
  height?: number;
  openDelay?: number;
  autoClose?: number;
  position?: "left" | "right";
  label?: string;
  buttonColor?: string;
  closeLabel?: string;
}

type OverlayMode = Exclude<EmbedMode, "standard">;

/** What the script exposes as window.formaEmbed. */
export interface FormaEmbed {
  /** Renders every embed element under root (the whole document by default) that is not rendered yet. A page that adds embed elements after the script has run calls it again. */
  scan: (root?: ParentNode) => void;
  /** Opens a survey as a popup (the default), slider or side tab from the page's own code. */
  open: (surveyUrl: string, options?: EmbedOptions & { mode?: OverlayMode }) => void;
  /** Closes whatever popup, slider or side tab is open. */
  close: () => void;
}

/** How tall a frame is before its survey has reported its own height. */
const DEFAULT_HEIGHT: Readonly<Record<"standard" | "popup" | "side-tab", number>> = {
  standard: 500,
  popup: 480,
  "side-tab": 420,
};

const DEFAULT_TITLE = "Survey";
const DEFAULT_CLOSE_LABEL = "Close";
const DEFAULT_TAB_LABEL = "Feedback";
const DEFAULT_TAB_COLOR = "#0f172a";

/** Above everything a page is likely to stack, below the 32-bit maximum some widgets claim. A side tab sits one below it, so an open popup or slider covers it like the rest of the page. */
const Z_INDEX = 2147483000;

// The overlays live in a shadow root, so the host page's styles (a global `button {}` or `iframe {}` rule) cannot reach them and theirs cannot leak out.
const OVERLAY_CSS = `
:host{all:initial}
.root{display:none}
.root.visible{display:block}
.backdrop{position:fixed;inset:0;z-index:${Z_INDEX};background:rgba(15,23,42,.5);opacity:0;transition:opacity .2s ease}
.open .backdrop{opacity:1}
.panel{position:fixed;z-index:${Z_INDEX + 1};box-sizing:border-box;transition:transform .25s ease,opacity .25s ease}
.box{background:#fff;overflow:hidden;box-shadow:0 12px 40px rgba(15,23,42,.3);height:100%}
iframe{display:block;width:100%;border:0}
.popup{top:50%;left:50%;width:min(640px,calc(100vw - 32px));opacity:0;transform:translate(-50%,-47%)}
.open .popup{opacity:1;transform:translate(-50%,-50%)}
.popup .box{border-radius:12px}
.popup iframe{max-height:calc(100vh - 96px);max-height:calc(100dvh - 96px)}
.popup .close{top:-40px;right:0}
.slider{top:0;bottom:0;width:min(440px,calc(100vw - 48px))}
.slider.right{right:0;transform:translateX(100%)}
.slider.left{left:0;transform:translateX(-100%)}
.open .slider{transform:none}
.slider iframe{height:100%}
.slider.right .close{top:12px;left:-44px}
.slider.left .close{top:12px;right:-44px}
.sidepanel{top:50%;width:min(400px,calc(100vw - 64px));opacity:0;transform:translateY(-50%) scale(.96)}
.sidepanel.right{right:52px;transform-origin:right center}
.sidepanel.left{left:52px;transform-origin:left center}
.open .sidepanel{opacity:1;transform:translateY(-50%)}
.sidepanel .box{border-radius:12px}
.sidepanel iframe{max-height:min(640px,calc(100vh - 32px));max-height:min(640px,calc(100dvh - 32px))}
.close{position:absolute;width:32px;height:32px;padding:0;border:0;border-radius:16px;background:#fff;color:#334155;cursor:pointer;box-shadow:0 2px 8px rgba(15,23,42,.25);font:22px/32px system-ui,sans-serif;text-align:center}
.close:focus-visible,.tab:focus-visible{outline:2px solid #2563eb;outline-offset:2px}
.tab{position:fixed;top:50%;z-index:${Z_INDEX - 1};padding:16px 10px;border:0;color:#fff;cursor:pointer;font:600 14px/1.2 system-ui,sans-serif;writing-mode:vertical-rl;box-shadow:0 2px 12px rgba(15,23,42,.25)}
.tab.right{right:0;border-radius:0 8px 8px 0;transform:translateY(-50%) rotate(180deg)}
.tab.left{left:0;border-radius:8px 0 0 8px;transform:translateY(-50%) rotate(180deg)}
.sentinel{position:fixed;width:1px;height:1px;overflow:hidden;clip-path:inset(50%)}
@media (prefers-reduced-motion:reduce){.backdrop,.panel{transition:none}}
`;

interface Frame {
  iframe: HTMLIFrameElement;
  origin: string;
  onResize: (height: number) => void;
  onCompleted: () => void;
  /** The respondent pressed Escape inside the survey. */
  onEscape: () => void;
}

/** A number of seconds or pixels from an attribute: a finite, non-negative number, or nothing. */
const readNumber = (value: string | null | undefined): number | undefined => {
  if (value === null || value === undefined || value.trim() === "") return undefined;
  const number = Number(value);
  return Number.isFinite(number) && number >= 0 ? number : undefined;
};

/** The options an embed element carries. */
export const readOptions = (element: Element): EmbedOptions => {
  const text = (name: string) => element.getAttribute(name)?.trim() || undefined;
  return {
    title: text(OPTION_ATTRIBUTES.title),
    height: readNumber(element.getAttribute(OPTION_ATTRIBUTES.height)),
    openDelay: readNumber(element.getAttribute(OPTION_ATTRIBUTES.openDelay)),
    autoClose: readNumber(element.getAttribute(OPTION_ATTRIBUTES.autoClose)),
    position: text(OPTION_ATTRIBUTES.position) === "left" ? "left" : "right",
    label: text(OPTION_ATTRIBUTES.label),
    buttonColor: text(OPTION_ATTRIBUTES.buttonColor),
    closeLabel: text(OPTION_ATTRIBUTES.closeLabel),
  };
};

/** The key that remembers, for the browser session, that a survey already opened on its own. */
const autoOpenedKey = (url: URL) => `${AUTO_OPENED_KEY_PREFIX}${url.origin}${url.pathname}`;

/** Creates the embed script's API for a window. Every embed it renders reports through one message listener, which only believes a frame's own window from its survey's origin. */
export const createFormaEmbed = (win: Window): FormaEmbed => {
  const doc = win.document;
  const frames = new Set<Frame>();
  const rendered = new WeakSet<Element>();
  const programmatic = new Map<string, Overlay>();
  let openOverlay: Overlay | null = null;

  win.addEventListener("message", (event: MessageEvent) => {
    for (const frame of frames) {
      if (event.source !== frame.iframe.contentWindow || event.origin !== frame.origin) continue;
      // A survey page that has not rendered yet measures nothing; keeping the frame's height until it has avoids a frame that collapses and grows back.
      if (isResizeMessage(event.data) && event.data.height > 0) frame.onResize(Math.ceil(event.data.height));
      else if (event.data === COMPLETED_MESSAGE) frame.onCompleted();
      else if (isEscapeMessage(event.data)) frame.onEscape();
    }
  });

  // Escape while focus is on the page; while it is inside a survey's frame, the survey page posts an escape message instead (Frame.onEscape).
  doc.addEventListener("keydown", (event: KeyboardEvent) => {
    if (event.key === "Escape" && openOverlay) openOverlay.close();
  });

  /** The element that has focus, inside shadow roots too: the document only knows the shadow host, which is not focusable itself (a side tab's own tab lives in the overlay's shadow root). */
  const focusedElement = (): Element | null => {
    let active = doc.activeElement;
    while (active?.shadowRoot?.activeElement) active = active.shadowRoot.activeElement;
    return active;
  };

  const createFrame = (url: URL, title: string): HTMLIFrameElement => {
    const iframe = doc.createElement("iframe");
    iframe.src = url.toString();
    iframe.title = title;
    return iframe;
  };

  const dispatchCompleted = (target: EventTarget, url: URL) => {
    target.dispatchEvent(
      new CustomEvent(COMPLETED_EVENT, { bubbles: true, detail: { url: url.toString() } })
    );
  };

  const mountStandard = (host: HTMLElement, url: URL, options: EmbedOptions) => {
    const iframe = createFrame(url, options.title ?? DEFAULT_TITLE);
    // A frame below the fold need not load (and count a display) until the respondent scrolls to it.
    iframe.loading = "lazy";
    Object.assign(iframe.style, {
      display: "block",
      width: "100%",
      border: "0",
      height: `${options.height ?? DEFAULT_HEIGHT.standard}px`,
    });
    // Whatever the element held (a plain link to the survey for pages without JavaScript) gives way to the survey.
    host.replaceChildren(iframe);
    frames.add({
      iframe,
      origin: url.origin,
      onResize: (height) => {
        iframe.style.height = `${height}px`;
      },
      onCompleted: () => dispatchCompleted(host, url),
      // Nothing to close: the survey sits in the page.
      onEscape: () => {},
    });
  };

  interface Overlay {
    open: () => void;
    close: () => void;
  }

  /**
   * A popup, slider or side tab. Its frame is created on the first opening, so a visitor who never opens it never loads the survey (or counts as a display), and kept after closing, so reopening resumes where the respondent left off.
   *
   * trigger is the page's element that opens it (aria-expanded follows the overlay, focus returns to it), if any. eventTarget is the page's element that announces the completed survey: the embed element for every layout, a side tab's too though the script draws the tab itself, or the overlay's own host for formaEmbed.open(), which has no element on the page.
   */
  const createOverlay = (
    mode: OverlayMode,
    url: URL,
    options: EmbedOptions,
    trigger: HTMLElement | null,
    eventTarget: HTMLElement | null
  ): Overlay => {
    const position = options.position ?? "right";
    const title = options.title ?? DEFAULT_TITLE;
    const isModal = mode !== "side-tab";

    const host = doc.createElement("div");
    host.setAttribute("data-forma-overlay", mode);
    const shadow = host.attachShadow({ mode: "open" });
    const style = doc.createElement("style");
    style.textContent = OVERLAY_CSS;
    const root = doc.createElement("div");
    root.className = "root";
    shadow.append(style, root);

    let panel: HTMLElement | null = null;
    let iframe: HTMLIFrameElement | null = null;
    let closeButton: HTMLButtonElement | null = null;
    let isOpen = false;
    let returnFocus: Element | null = null;
    let previousOverflow = "";
    let autoCloseTimer: ReturnType<typeof setTimeout> | undefined;

    const tab =
      mode === "side-tab"
        ? (() => {
            const button = doc.createElement("button");
            button.type = "button";
            button.className = `tab ${position}`;
            button.textContent = options.label ?? DEFAULT_TAB_LABEL;
            button.style.background = options.buttonColor ?? DEFAULT_TAB_COLOR;
            button.setAttribute("aria-haspopup", "dialog");
            button.setAttribute("aria-expanded", "false");
            button.addEventListener("click", () => {
              if (isOpen) overlay.close();
              else overlay.open();
            });
            shadow.append(button);
            return button;
          })()
        : null;

    const setExpanded = (expanded: boolean) => {
      for (const control of [trigger, tab]) control?.setAttribute("aria-expanded", String(expanded));
    };

    const build = () => {
      const panelElement = doc.createElement("div");
      panelElement.className = `panel ${mode === "side-tab" ? "sidepanel" : mode} ${position}`;
      panelElement.setAttribute("role", "dialog");
      panelElement.setAttribute("aria-label", title);
      if (isModal) panelElement.setAttribute("aria-modal", "true");

      const box = doc.createElement("div");
      box.className = "box";
      const frame = createFrame(url, title);
      if (mode !== "slider") frame.style.height = `${DEFAULT_HEIGHT[mode]}px`;
      box.append(frame);

      if (isModal) {
        const backdrop = doc.createElement("div");
        backdrop.className = "backdrop";
        backdrop.addEventListener("click", () => overlay.close());
        const button = doc.createElement("button");
        button.type = "button";
        button.className = "close";
        button.setAttribute("aria-label", options.closeLabel ?? DEFAULT_CLOSE_LABEL);
        button.textContent = "×";
        button.addEventListener("click", () => overlay.close());
        // Focus stays in the dialog: tabbing past either end of it lands back inside instead of on the page behind.
        const sentinel = (target: () => HTMLElement) => {
          const element = doc.createElement("span");
          element.className = "sentinel";
          element.tabIndex = 0;
          element.addEventListener("focus", () => target().focus());
          return element;
        };
        panelElement.append(
          sentinel(() => frame),
          button,
          box,
          sentinel(() => button)
        );
        root.append(backdrop, panelElement);
        closeButton = button;
      } else {
        panelElement.append(box);
        root.append(panelElement);
      }

      frames.add({
        iframe: frame,
        origin: url.origin,
        onResize: (height) => {
          // The slider is as tall as the window whatever the survey's height; its frame scrolls.
          if (mode !== "slider") frame.style.height = `${height}px`;
        },
        onCompleted: () => {
          dispatchCompleted(eventTarget ?? host, url);
          if (options.autoClose !== undefined) {
            clearTimeout(autoCloseTimer);
            autoCloseTimer = setTimeout(() => overlay.close(), options.autoClose * 1000);
          }
        },
        onEscape: () => overlay.close(),
      });
      panel = panelElement;
      iframe = frame;
    };

    const overlay: Overlay = {
      open: () => {
        if (isOpen) return;
        if (openOverlay && openOverlay !== overlay) openOverlay.close();
        if (!panel) build();
        isOpen = true;
        openOverlay = overlay;
        returnFocus = focusedElement();
        root.classList.add("visible");
        // Read layout once so the entry transition starts from the hidden state.
        void panel?.offsetWidth;
        root.classList.add("open");
        setExpanded(true);
        if (isModal) {
          previousOverflow = doc.documentElement.style.overflow;
          doc.documentElement.style.overflow = "hidden";
          closeButton?.focus();
        } else {
          iframe?.focus();
        }
      },
      close: () => {
        if (!isOpen) return;
        isOpen = false;
        clearTimeout(autoCloseTimer);
        if (openOverlay === overlay) openOverlay = null;
        root.classList.remove("open", "visible");
        setExpanded(false);
        if (isModal) doc.documentElement.style.overflow = previousOverflow;
        (returnFocus as HTMLElement | null)?.focus?.();
        returnFocus = null;
      },
    };

    doc.body.append(host);

    if (options.openDelay !== undefined) {
      setTimeout(() => {
        if (isOpen || rememberAutoOpen(url)) return;
        overlay.open();
      }, options.openDelay * 1000);
    }

    return overlay;
  };

  /** Records that the survey opened on its own in this browser session, and reports whether it already had: a survey that opens on a delay does not open again on every page the visitor goes to. */
  const rememberAutoOpen = (url: URL): boolean => {
    const key = autoOpenedKey(url);
    try {
      if (win.sessionStorage.getItem(key)) return true;
      win.sessionStorage.setItem(key, "1");
    } catch {
      // Storage refused (a sandboxed frame, Safari's private mode on old versions): open every time rather than never.
    }
    return false;
  };

  const mount = (element: HTMLElement, mode: EmbedMode) => {
    if (rendered.has(element)) return;
    const url = embedFrameUrl(element.getAttribute(MODE_ATTRIBUTES[mode]) ?? "", doc.baseURI);
    if (!url) return;
    rendered.add(element);
    const options = readOptions(element);
    if (mode === "standard") {
      mountStandard(element, url, options);
      return;
    }
    const isTrigger = mode !== "side-tab";
    const overlay = createOverlay(mode, url, options, isTrigger ? element : null, element);
    if (isTrigger) {
      element.setAttribute("aria-haspopup", "dialog");
      element.setAttribute("aria-expanded", "false");
      element.addEventListener("click", (event) => {
        // A link to the survey stays the fallback for pages without JavaScript; with it, the survey opens here instead.
        event.preventDefault();
        overlay.open();
      });
    }
  };

  return {
    scan: (root: ParentNode = doc) => {
      for (const mode of Object.keys(MODE_ATTRIBUTES) as EmbedMode[]) {
        for (const element of root.querySelectorAll<HTMLElement>(`[${MODE_ATTRIBUTES[mode]}]`)) {
          mount(element, mode);
        }
      }
    },
    open: (surveyUrl, options = {}) => {
      const mode = options.mode ?? "popup";
      const url = embedFrameUrl(surveyUrl, doc.baseURI);
      if (!url) return;
      const key = `${mode} ${url.toString()}`;
      let overlay = programmatic.get(key);
      if (!overlay) {
        overlay = createOverlay(mode, url, { ...options, openDelay: undefined }, null, null);
        programmatic.set(key, overlay);
      }
      overlay.open();
    },
    close: () => {
      openOverlay?.close();
    },
  };
};
