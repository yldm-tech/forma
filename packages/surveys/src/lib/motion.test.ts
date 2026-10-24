// @vitest-environment happy-dom
import { afterEach, describe, expect, test, vi } from "vite-plus/test";
import { prefersReducedMotion, scrollBehavior, stackedCardTransition } from "./motion";

const stubReducedMotion = (reduce: boolean) => {
  vi.stubGlobal(
    "matchMedia",
    vi.fn((query: string) => ({ matches: reduce && query === "(prefers-reduced-motion: reduce)" }))
  );
};

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("reduced motion", () => {
  test("by default the cards slide and errors scroll smoothly", () => {
    stubReducedMotion(false);
    expect(prefersReducedMotion()).toBe(false);
    expect(scrollBehavior()).toBe("smooth");
    expect(stackedCardTransition()).toContain("transform 600ms");
  });

  test("with reduced motion the cards only fade and errors jump into view", () => {
    stubReducedMotion(true);
    expect(prefersReducedMotion()).toBe(true);
    expect(scrollBehavior()).toBe("auto");
    expect(stackedCardTransition()).toBe("opacity 150ms ease-in-out");
  });
});
