/**
 * @vitest-environment jsdom
 */
import { RESIZE_MESSAGE_TYPE } from "@forma/embed/protocol";
import { afterEach, describe, expect, test, vi } from "vite-plus/test";
import { readEmbedFrameMode, reportEmbedHeight } from "./embed-frame";

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
  });

  const stubResizeObserver = () => {
    vi.stubGlobal(
      "ResizeObserver",
      class {
        constructor(callback: () => void) {
          observed = callback;
        }
        observe() {}
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
});
