import { afterEach, describe, expect, test, vi } from "vite-plus/test";
import { isValidCssSelector } from "./action-class";

// The SPA's tests run without a DOM; each test stands in a document whose element accepts or rejects the selector.
const stubDocument = (querySelector: () => unknown) => {
  vi.stubGlobal("document", { createElement: vi.fn(() => ({ querySelector: vi.fn(querySelector) })) });
};

describe("isValidCssSelector", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  test("should return false for undefined selector", () => {
    expect(isValidCssSelector(undefined)).toBe(false);
  });

  test("should return false for empty string", () => {
    expect(isValidCssSelector("")).toBe(false);
  });

  test("should return true for valid CSS selector", () => {
    stubDocument(() => null);
    expect(isValidCssSelector(".class")).toBe(true);
    expect(isValidCssSelector("#id")).toBe(true);
    expect(isValidCssSelector("div")).toBe(true);
  });

  test("should return false for invalid CSS selector", () => {
    stubDocument(() => {
      throw new Error("Invalid selector");
    });
    expect(isValidCssSelector("..invalid")).toBe(false);
    expect(isValidCssSelector("##invalid")).toBe(false);
  });
});
