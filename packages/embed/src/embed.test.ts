import { afterEach, describe, expect, test, vi } from "vite-plus/test";
import { createFormaEmbed } from "./embed";
import {
  COMPLETED_EVENT,
  COMPLETED_MESSAGE,
  RESIZE_MESSAGE_TYPE,
  embedFrameUrl,
  isResizeMessage,
} from "./protocol";

const SURVEY = "https://forma.example.com/s/0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d12";

afterEach(() => {
  document.body.replaceChildren();
  document.documentElement.style.overflow = "";
  sessionStorage.clear();
  vi.useRealTimers();
});

/** A message from a frame's survey page, as the browser delivers it to the page around it. */
const post = (iframe: HTMLIFrameElement, data: unknown, origin = "https://forma.example.com") => {
  window.dispatchEvent(new MessageEvent("message", { data, origin, source: iframe.contentWindow }));
};

const overlayRoot = (mode: string) => {
  const host = document.querySelector<HTMLElement>(`[data-forma-overlay="${mode}"]`);
  if (!host?.shadowRoot) throw new Error(`no ${mode} overlay`);
  return host.shadowRoot;
};

const render = (html: string) => {
  document.body.innerHTML = html;
  const embed = createFormaEmbed(window);
  embed.scan();
  return embed;
};

describe("embedFrameUrl", () => {
  test("embeds the link at its content's height and keeps what it carries", () => {
    expect(embedFrameUrl(`${SURVEY}?lang=de&source=newsletter`)?.toString()).toBe(
      `${SURVEY}?lang=de&source=newsletter&embed=true&autoHeight=true`
    );
  });

  test("refuses anything but an http(s) link", () => {
    expect(embedFrameUrl("javascript:alert(1)")).toBeNull();
    expect(embedFrameUrl("data:text/html,<p>x</p>")).toBeNull();
    expect(embedFrameUrl("http://[bad")).toBeNull();
  });
});

describe("isResizeMessage", () => {
  test("accepts only a finite, non-negative height", () => {
    expect(isResizeMessage({ type: RESIZE_MESSAGE_TYPE, height: 640 })).toBe(true);
    expect(isResizeMessage({ type: RESIZE_MESSAGE_TYPE, height: -1 })).toBe(false);
    expect(isResizeMessage({ type: RESIZE_MESSAGE_TYPE, height: Number.NaN })).toBe(false);
    expect(isResizeMessage({ type: RESIZE_MESSAGE_TYPE, height: "640" })).toBe(false);
    expect(isResizeMessage({ type: "other", height: 640 })).toBe(false);
    expect(isResizeMessage(COMPLETED_MESSAGE)).toBe(false);
  });
});

describe("the standard embed", () => {
  test("replaces the element's fallback with a frame that follows the survey's height", () => {
    render(
      `<div id="host" data-forma-embed="${SURVEY}" data-forma-title="Feedback survey"><a href="${SURVEY}">Open</a></div>`
    );
    const iframe = document.querySelector("#host iframe") as HTMLIFrameElement;
    expect(document.querySelector("#host a")).toBeNull();
    expect(iframe.src).toBe(`${SURVEY}?embed=true&autoHeight=true`);
    expect(iframe.title).toBe("Feedback survey");
    expect(iframe.style.height).toBe("500px");

    post(iframe, { type: RESIZE_MESSAGE_TYPE, height: 731.2 });
    expect(iframe.style.height).toBe("732px");
  });

  test("starts at the height the element asks for", () => {
    render(`<div id="host" data-forma-embed="${SURVEY}" data-forma-height="320"></div>`);
    expect((document.querySelector("#host iframe") as HTMLIFrameElement).style.height).toBe("320px");
  });

  test("believes only its own frame, from its survey's origin", () => {
    render(`<div id="a" data-forma-embed="${SURVEY}"></div><div id="b" data-forma-embed="${SURVEY}"></div>`);
    const a = document.querySelector("#a iframe") as HTMLIFrameElement;
    const b = document.querySelector("#b iframe") as HTMLIFrameElement;

    post(a, { type: RESIZE_MESSAGE_TYPE, height: 900 }, "https://evil.example.com");
    expect(a.style.height).toBe("500px");

    post(a, { type: RESIZE_MESSAGE_TYPE, height: 900 });
    expect(a.style.height).toBe("900px");
    expect(b.style.height).toBe("500px");
  });

  test("keeps its height while the survey page has not rendered", () => {
    render(`<div id="host" data-forma-embed="${SURVEY}"></div>`);
    const iframe = document.querySelector("#host iframe") as HTMLIFrameElement;
    post(iframe, { type: RESIZE_MESSAGE_TYPE, height: 0 });
    expect(iframe.style.height).toBe("500px");
  });

  test("announces the completed survey on its element", () => {
    render(`<section id="outer"><div id="host" data-forma-embed="${SURVEY}"></div></section>`);
    const completed = vi.fn();
    document.querySelector("#outer")?.addEventListener(COMPLETED_EVENT, completed);
    post(document.querySelector("#host iframe") as HTMLIFrameElement, COMPLETED_MESSAGE);
    expect(completed).toHaveBeenCalledOnce();
    expect((completed.mock.calls[0][0] as CustomEvent).detail).toEqual({
      url: `${SURVEY}?embed=true&autoHeight=true`,
    });
  });

  test("renders an element once however often the page scans", () => {
    const embed = render(`<div id="host" data-forma-embed="${SURVEY}"></div>`);
    const iframe = document.querySelector("#host iframe");
    embed.scan();
    expect(document.querySelectorAll("#host iframe")).toHaveLength(1);
    expect(document.querySelector("#host iframe")).toBe(iframe);
  });

  test("leaves an element whose link is not http(s) alone", () => {
    render(`<div id="host" data-forma-embed="javascript:alert(1)"><a>Fallback</a></div>`);
    expect(document.querySelector("#host iframe")).toBeNull();
    expect(document.querySelector("#host a")).not.toBeNull();
  });
});

describe("the popup", () => {
  test("loads the survey only when opened, as a modal dialog, and gives focus back on Escape", () => {
    render(
      `<a id="trigger" href="${SURVEY}" data-forma-popup="${SURVEY}" data-forma-title="Quick survey">Take the survey</a>`
    );
    const trigger = document.querySelector<HTMLAnchorElement>("#trigger")!;
    const shadow = overlayRoot("popup");
    expect(shadow.querySelector("iframe")).toBeNull();
    expect(trigger.getAttribute("aria-haspopup")).toBe("dialog");

    trigger.focus();
    const click = new MouseEvent("click", { bubbles: true, cancelable: true });
    trigger.dispatchEvent(click);
    expect(click.defaultPrevented).toBe(true);

    const dialog = shadow.querySelector('[role="dialog"]')!;
    expect(dialog.getAttribute("aria-modal")).toBe("true");
    expect(dialog.getAttribute("aria-label")).toBe("Quick survey");
    expect(shadow.querySelector(".root")?.classList.contains("visible")).toBe(true);
    expect(trigger.getAttribute("aria-expanded")).toBe("true");
    expect(shadow.activeElement).toBe(shadow.querySelector(".close"));
    expect(document.documentElement.style.overflow).toBe("hidden");

    const iframe = shadow.querySelector("iframe")!;
    post(iframe, { type: RESIZE_MESSAGE_TYPE, height: 610 });
    expect(iframe.style.height).toBe("610px");

    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    expect(shadow.querySelector(".root")?.classList.contains("visible")).toBe(false);
    expect(trigger.getAttribute("aria-expanded")).toBe("false");
    expect(document.activeElement).toBe(trigger);
    expect(document.documentElement.style.overflow).toBe("");

    trigger.click();
    expect(shadow.querySelectorAll("iframe")).toHaveLength(1);
    expect(shadow.querySelector("iframe")).toBe(iframe);
  });

  test("closes from its close button and its backdrop", () => {
    render(
      `<button id="trigger" data-forma-popup="${SURVEY}" data-forma-close-label="Schließen">Open</button>`
    );
    const trigger = document.querySelector<HTMLButtonElement>("#trigger")!;
    const shadow = overlayRoot("popup");
    const isVisible = () => shadow.querySelector(".root")?.classList.contains("visible");

    trigger.click();
    const close = shadow.querySelector<HTMLButtonElement>(".close")!;
    expect(close.getAttribute("aria-label")).toBe("Schließen");
    close.click();
    expect(isVisible()).toBe(false);

    trigger.click();
    shadow.querySelector<HTMLElement>(".backdrop")!.click();
    expect(isVisible()).toBe(false);
  });

  test("opens on its own after its delay, once per browser session", () => {
    vi.useFakeTimers();
    render(`<div data-forma-popup="${SURVEY}" data-forma-open-delay="5"></div>`);
    const isVisible = () => overlayRoot("popup").querySelector(".root")?.classList.contains("visible");
    vi.advanceTimersByTime(4999);
    expect(isVisible()).toBe(false);
    vi.advanceTimersByTime(1);
    expect(isVisible()).toBe(true);

    // The visitor goes to another page of the same site.
    render(`<div data-forma-popup="${SURVEY}" data-forma-open-delay="5"></div>`);
    vi.advanceTimersByTime(5000);
    expect(isVisible()).toBe(false);
  });

  test("closes the given number of seconds after the survey is completed", () => {
    vi.useFakeTimers();
    render(`<button id="trigger" data-forma-popup="${SURVEY}" data-forma-auto-close="3">Open</button>`);
    const trigger = document.querySelector<HTMLButtonElement>("#trigger")!;
    const completed = vi.fn();
    trigger.addEventListener(COMPLETED_EVENT, completed);
    const shadow = overlayRoot("popup");

    trigger.click();
    post(shadow.querySelector("iframe")!, COMPLETED_MESSAGE);
    expect(completed).toHaveBeenCalledOnce();
    vi.advanceTimersByTime(2999);
    expect(shadow.querySelector(".root")?.classList.contains("visible")).toBe(true);
    vi.advanceTimersByTime(1);
    expect(shadow.querySelector(".root")?.classList.contains("visible")).toBe(false);
  });
});

describe("the slider", () => {
  test("slides in from the edge it names and fills the window's height", () => {
    render(`<button id="trigger" data-forma-slider="${SURVEY}" data-forma-position="left">Open</button>`);
    document.querySelector<HTMLButtonElement>("#trigger")!.click();
    const shadow = overlayRoot("slider");
    const panel = shadow.querySelector('[role="dialog"]')!;
    expect(panel.classList.contains("slider")).toBe(true);
    expect(panel.classList.contains("left")).toBe(true);

    const iframe = shadow.querySelector("iframe")!;
    post(iframe, { type: RESIZE_MESSAGE_TYPE, height: 300 });
    expect(iframe.style.height).toBe("");
  });
});

describe("the side tab", () => {
  test("puts a labelled tab on the edge that opens and closes a panel beside it", () => {
    render(
      `<div data-forma-side-tab="${SURVEY}" data-forma-label="Give feedback" data-forma-button-color="#7c3aed"></div>`
    );
    const shadow = overlayRoot("side-tab");
    const tab = shadow.querySelector<HTMLButtonElement>(".tab")!;
    expect(tab.textContent).toBe("Give feedback");
    expect(tab.style.background).toBe("rgb(124, 58, 237)");
    expect(tab.classList.contains("right")).toBe(true);
    expect(shadow.querySelector("iframe")).toBeNull();

    tab.click();
    const panel = shadow.querySelector('[role="dialog"]')!;
    expect(panel.hasAttribute("aria-modal")).toBe(false);
    expect(tab.getAttribute("aria-expanded")).toBe("true");
    // Non-modal: the page behind keeps scrolling.
    expect(document.documentElement.style.overflow).toBe("");

    tab.click();
    expect(shadow.querySelector(".root")?.classList.contains("visible")).toBe(false);
    expect(tab.getAttribute("aria-expanded")).toBe("false");
  });
});

describe("formaEmbed.open", () => {
  test("opens a survey from the page's own code and closes it again", () => {
    const embed = render("");
    embed.open(SURVEY, { mode: "slider" });
    const shadow = overlayRoot("slider");
    expect(shadow.querySelector("iframe")?.src).toBe(`${SURVEY}?embed=true&autoHeight=true`);
    expect(shadow.querySelector(".root")?.classList.contains("visible")).toBe(true);

    embed.close();
    expect(shadow.querySelector(".root")?.classList.contains("visible")).toBe(false);

    embed.open(SURVEY, { mode: "slider" });
    expect(document.querySelectorAll('[data-forma-overlay="slider"]')).toHaveLength(1);
  });

  test("closes the open survey before opening another", () => {
    const embed = render(`<button id="trigger" data-forma-popup="${SURVEY}">Open</button>`);
    document.querySelector<HTMLButtonElement>("#trigger")!.click();
    embed.open(`${SURVEY}?lang=de`);
    const popups = [...document.querySelectorAll<HTMLElement>('[data-forma-overlay="popup"]')];
    expect(
      popups.map((host) => host.shadowRoot?.querySelector(".root")?.classList.contains("visible"))
    ).toEqual([false, true]);
  });
});
