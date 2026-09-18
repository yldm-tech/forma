/// <reference lib="dom" />
/**
 * The browser implementation, selected by the `browser` condition in this package's `exports` map.
 * See `html-text.ts` for why the two exist and what they must agree on.
 *
 * `DOMParser` is a browser built-in, so this file adds nothing to the bundle. It is deliberately not
 * a runtime `typeof window` branch inside one module: a static `import { parse } from
 * "node-html-parser"` is bundled whether or not the branch that uses it ever runs.
 *
 * If a bundler ever fails to apply the condition and loads this on the server, the guards below keep
 * the output sane — tags are still stripped — rather than leaking raw markup into a summary export.
 */

const hasDomParser = (): boolean => typeof DOMParser !== "undefined";

/**
 * `node-html-parser` turns a `<br>` into a newline and `DOMParser` does not. They have to agree:
 * this value reaches CSV exports and integration payloads from the server and the same labels on
 * screen from the browser, and a difference between them is a hydration mismatch waiting to happen.
 * The server's behaviour wins because its output is the one that gets persisted.
 */
const BR_TAG = /<br\s*\/?>/gi;

/** Last resort when there is no DOM: strip tags textually, matching `getTextContent`'s contract. */
const stripTags = (str: string): string => str.replace(/<[^>]*>/g, "").trim();

/**
 * Checks if a string contains valid HTML markup
 * @param str - The input string to test
 * @returns true if the string contains valid HTML elements, false otherwise
 */
export const isValidHTML = (str: string): boolean => {
  if (!str) return false;
  if (!hasDomParser()) return /<[a-z][^>]*>/i.test(str);

  try {
    const doc = new DOMParser().parseFromString(str, "text/html");
    if (doc.querySelector("parsererror")) return false;
    // nodeType 1 = ELEMENT_NODE, matching the server implementation's check.
    return Array.from(doc.body.childNodes).some((node: Node) => node.nodeType === 1);
  } catch {
    return false;
  }
};

/**
 * Extracts text content from an HTML string
 * @param str - The input string (can be HTML or plain text)
 * @returns The extracted text content without HTML tags
 */
export const getTextContent = (str: string): string => {
  if (!str || str.trim() === "") return "";

  if (isValidHTML(str)) {
    if (!hasDomParser()) return stripTags(str);

    try {
      const withBreaks = str.replace(BR_TAG, "\n");
      return new DOMParser().parseFromString(withBreaks, "text/html").body.textContent.trim();
    } catch {
      // If parsing fails, treat as plain text
      return str.trim();
    }
  }

  return str.trim();
};
