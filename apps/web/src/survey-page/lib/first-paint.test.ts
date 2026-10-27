/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, test, vi } from "vite-plus/test";
import {
  FIRST_PAINT_DONE_CLASS,
  FIRST_PAINT_ID,
  fadeOutFirstPaint,
  hasFirstPaint,
  removeFirstPaint,
} from "./first-paint";

const paint = () => {
  document.body.innerHTML = `<div id="${FIRST_PAINT_ID}"><p dir="auto">Feedback</p></div><div id="root"></div>`;
  return document.getElementById(FIRST_PAINT_ID);
};

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  document.body.innerHTML = "";
});

describe("the server's first paint", () => {
  test("is found by the id the server writes", () => {
    expect(hasFirstPaint()).toBe(false);
    paint();
    expect(hasFirstPaint()).toBe(true);
  });

  test("is removed at once for another screen", () => {
    paint();
    removeFirstPaint();
    expect(hasFirstPaint()).toBe(false);
    // Nothing to remove is not an error.
    removeFirstPaint();
  });

  test("fades out, then leaves the document when its fade ends", () => {
    const element = paint();
    fadeOutFirstPaint(300);
    expect(element?.classList.contains(FIRST_PAINT_DONE_CLASS)).toBe(true);
    expect(hasFirstPaint()).toBe(true);
    element?.dispatchEvent(new Event("transitionend"));
    expect(hasFirstPaint()).toBe(false);
  });

  test("leaves the document after its fade even when no transitionend fires", () => {
    paint();
    fadeOutFirstPaint(300);
    vi.advanceTimersByTime(349);
    expect(hasFirstPaint()).toBe(true);
    vi.advanceTimersByTime(1);
    expect(hasFirstPaint()).toBe(false);
  });

  test("a page without it fades nothing", () => {
    expect(() => fadeOutFirstPaint(300)).not.toThrow();
  });
});
