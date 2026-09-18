// @vitest-environment jsdom
import { describe, expect, test } from "vitest";
import * as server from "./html-text";
import * as browser from "./html-text.browser";

/**
 * The two implementations are selected by an `exports` condition, so nothing at a call site says
 * which one it got. That only holds up if they agree, and the split exists to keep 84 KB brotli of
 * HTML entity tables out of the browser — a regression here is invisible until a headline renders
 * with its tags showing.
 *
 * The browser file is exercised against jsdom's `DOMParser`, which is the same API a real browser
 * provides for these two calls.
 */

const CASES: readonly (readonly [label: string, input: string, text: string, isHtml: boolean])[] = [
  ["plain text", "How satisfied are you?", "How satisfied are you?", false],
  ["a wrapped headline", "<p>How satisfied are you?</p>", "How satisfied are you?", true],
  ["nested markup", "<p>Rate <strong>our</strong> support</p>", "Rate our support", true],
  ["an entity", "<p>Tom &amp; Jerry</p>", "Tom & Jerry", true],
  ["a bare entity in plain text", "Tom &amp; Jerry", "Tom &amp; Jerry", false],
  ["surrounding whitespace", "  <p> padded </p>  ", "padded", true],
  ["an empty string", "", "", false],
  ["only whitespace", "   ", "", false],
  // `node-html-parser` turns a <br> into a newline; the browser file is made to match it.
  ["a line break", "<p>line<br/>break</p>", "line\nbreak", true],
  ["an attribute that looks like a tag", '<p title="<b>">x</p>', "x", true],
  ["multiple siblings", "<p>a</p><p>b</p>", "ab", true],
];

describe.each([
  ["server", server],
  ["browser", browser],
])("%s implementation", (_name, impl) => {
  test.each(CASES)("getTextContent: %s", (_label, input, text) => {
    expect(impl.getTextContent(input)).toBe(text);
  });

  test.each(CASES)("isValidHTML: %s", (_label, input, _text, isHtml) => {
    expect(impl.isValidHTML(input)).toBe(isHtml);
  });
});

describe("the browser implementation without a DOM", () => {
  // If a bundler ever fails to apply the `browser` condition and this runs on the server, tags must
  // still come off. Leaking raw markup into a summary export or an integration field is the failure
  // this guards against.
  const withoutDomParser = (run: () => void) => {
    const original = globalThis.DOMParser;
    // @ts-expect-error -- deleting a global to simulate a non-browser runtime
    delete globalThis.DOMParser;
    try {
      run();
    } finally {
      globalThis.DOMParser = original;
    }
  };

  test("still strips tags", () => {
    withoutDomParser(() => {
      expect(browser.getTextContent("<p>Rate <strong>our</strong> support</p>")).toBe("Rate our support");
    });
  });

  test("still recognises markup", () => {
    withoutDomParser(() => {
      expect(browser.isValidHTML("<p>x</p>")).toBe(true);
      expect(browser.isValidHTML("plain")).toBe(false);
    });
  });
});

describe("the exports map keeps node-html-parser out of the browser", () => {
  test("only the server implementation imports it", async () => {
    const { readFileSync } = await import("node:fs");
    const { join } = await import("node:path");
    // Vitest's root for this package. Asserted so a wrong path fails loudly instead of reading
    // nothing and passing.
    const here = join(process.cwd(), "surveys");
    expect(readFileSync(join(here, "html-text.ts"), "utf8").length).toBeGreaterThan(100);
    const read = (file: string) => readFileSync(join(here, file), "utf8");

    const importsIt = (source: string) => /^import .*from "node-html-parser";$/m.test(source);

    expect(importsIt(read("html-text.ts"))).toBe(true);
    expect(importsIt(read("html-text.browser.ts"))).toBe(false);
    // The forwarding module must not reach for it either, or the split buys nothing.
    expect(importsIt(read("validation.ts"))).toBe(false);
  });
});
