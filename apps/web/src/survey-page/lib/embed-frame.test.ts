/**
 * @vitest-environment jsdom
 */
import { ESCAPE_MESSAGE_TYPE, RESIZE_MESSAGE_TYPE } from "@forma/embed/protocol";
import { afterEach, describe, expect, test, vi } from "vite-plus/test";
import { readEmbedFrameMode, reportEmbedEscape, reportEmbedHeight } from "./embed-frame";

describe("readEmbedFrameMode", () => {
  test("takes the content's height only when embedded", () => {
    expect(readEmbedFrameMode("?embed=true&autoHeight=true")).toEqual({ isEmbed: true, isAutoHeight: true });
    expect(readEmbedFrameMode("?embed=true")).toEqual({ isEmbed: true, isAutoHeight: false });
    expect(readEmbedFrameMode("?autoHeight=true")).toEqual({ isEmbed: false, isAutoHeight: false });
    expect(readEmbedFrameMode("")).toEqual({ isEmbed: false, isAutoHeight: false });
  });
});

describe("reportEmbedHeight", () => {
  let observed: (() => void) | undefined;
  const disconnect = vi.fn();

  afterEach(() => {
    vi.unstubAllGlobals();
    observed = undefined;
    disconnect.mockReset();
    document.body.replaceChildren();
  });

  const stubResizeObserver = () => {
    vi.stubGlobal(
      "ResizeObserver",
      class {
        constructor(callback: () => void) {
          observed = callback;
        }
        observe() {}
        unobserve() {}
        disconnect() {
          disconnect();
        }
      }
    );
  };

  const elementOfHeight = () => {
    const element = document.createElement("div");
    let height = 0;
    element.getBoundingClientRect = () => ({ height }) as DOMRect;
    return { element, setHeight: (value: number) => (height = value) };
  };

  test("posts the height whenever it changes, never a height of nothing", () => {
    stubResizeObserver();
    const { element, setHeight } = elementOfHeight();
    const target = { postMessage: vi.fn() };

    const stop = reportEmbedHeight(element, target);
    // Not rendered yet: nothing to report.
    expect(target.postMessage).not.toHaveBeenCalled();

    setHeight(640.3);
    observed?.();
    expect(target.postMessage).toHaveBeenLastCalledWith({ type: RESIZE_MESSAGE_TYPE, height: 641 }, "*");

    observed?.();
    expect(target.postMessage).toHaveBeenCalledTimes(1);

    setHeight(380);
    observed?.();
    expect(target.postMessage).toHaveBeenLastCalledWith({ type: RESIZE_MESSAGE_TYPE, height: 380 }, "*");

    stop();
    expect(disconnect).toHaveBeenCalledOnce();
  });

  /** A dropdown as Radix draws it: portaled to the end of <body>, outside the survey's root, cut to the space the frame has left. */
  const openDropdown = ({ withTrigger }: { withTrigger: boolean }) => {
    if (withTrigger) {
      const trigger = document.createElement("button");
      trigger.setAttribute("aria-controls", "radix-:r1:");
      trigger.getBoundingClientRect = () => ({ bottom: 120 }) as DOMRect;
      document.body.append(trigger);
    }
    const wrapper = document.createElement("div");
    wrapper.setAttribute("data-radix-popper-content-wrapper", "");
    const content = document.createElement("div");
    content.id = "radix-:r1:";
    content.getBoundingClientRect = () => ({ top: 124 }) as DOMRect;
    // Cut to 121px of its natural 290px, with a 1px border.
    Object.defineProperties(content, {
      scrollHeight: { value: 290 },
      clientHeight: { value: 121 },
      offsetHeight: { value: 123 },
    });
    wrapper.append(content);
    document.body.append(wrapper);
    return wrapper;
  };

  // MutationObserver reports in a microtask.
  const settle = () => new Promise((resolve) => setTimeout(resolve, 0));

  test("grows to show an open dropdown's options below its trigger, and shrinks back when it closes", async () => {
    stubResizeObserver();
    const { element, setHeight } = elementOfHeight();
    setHeight(246);
    const target = { postMessage: vi.fn() };
    const stop = reportEmbedHeight(element, target);
    expect(target.postMessage).toHaveBeenLastCalledWith({ type: RESIZE_MESSAGE_TYPE, height: 246 }, "*");

    const wrapper = openDropdown({ withTrigger: true });
    await settle();
    // The trigger's bottom, the options' natural height with their border, and room for the gap and shadow.
    expect(target.postMessage).toHaveBeenLastCalledWith(
      { type: RESIZE_MESSAGE_TYPE, height: 120 + 292 + 8 },
      "*"
    );

    wrapper.remove();
    await settle();
    expect(target.postMessage).toHaveBeenLastCalledWith({ type: RESIZE_MESSAGE_TYPE, height: 246 }, "*");
    stop();
  });

  test("measures a floating layer without a trigger from where it is drawn", async () => {
    stubResizeObserver();
    const { element, setHeight } = elementOfHeight();
    setHeight(246);
    const target = { postMessage: vi.fn() };
    const stop = reportEmbedHeight(element, target);
    openDropdown({ withTrigger: false });
    await settle();
    expect(target.postMessage).toHaveBeenLastCalledWith(
      { type: RESIZE_MESSAGE_TYPE, height: 124 + 292 + 8 },
      "*"
    );
    stop();
  });

  test("never reports less than the survey's own height for a small dropdown", async () => {
    stubResizeObserver();
    const { element, setHeight } = elementOfHeight();
    setHeight(900);
    const target = { postMessage: vi.fn() };
    const stop = reportEmbedHeight(element, target);
    openDropdown({ withTrigger: true });
    await settle();
    expect(target.postMessage).toHaveBeenCalledOnce();
    expect(target.postMessage).toHaveBeenLastCalledWith({ type: RESIZE_MESSAGE_TYPE, height: 900 }, "*");
    stop();
  });
});

describe("reportEmbedEscape", () => {
  const press = (key: string, init: KeyboardEventInit = {}) =>
    document.body.dispatchEvent(
      new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true, ...init })
    );

  test("passes on an Escape nothing in the survey took", () => {
    const target = { postMessage: vi.fn() };
    const stop = reportEmbedEscape(window, target);

    press("Enter");
    press("Escape", { isComposing: true });
    expect(target.postMessage).not.toHaveBeenCalled();

    press("Escape");
    expect(target.postMessage).toHaveBeenCalledExactlyOnceWith({ type: ESCAPE_MESSAGE_TYPE }, "*");

    // An open dropdown closes on Escape and marks the key as handled (Radix's DismissableLayer, a capturing listener on the document).
    const dropdown = (event: KeyboardEvent) => event.preventDefault();
    document.addEventListener("keydown", dropdown, { capture: true });
    press("Escape");
    document.removeEventListener("keydown", dropdown, { capture: true });
    expect(target.postMessage).toHaveBeenCalledOnce();

    stop();
    press("Escape");
    expect(target.postMessage).toHaveBeenCalledOnce();
  });
});
