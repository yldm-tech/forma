import { parse } from "node-html-parser";

/**
 * The server implementation. `html-text.browser.ts` is the browser one, and the `exports` map in
 * this package's `package.json` picks between them.
 *
 * They are split because `node-html-parser` drags the full HTML entity tables with it — `he` at
 * 85 KB plus `entities` at 69 KB, 221 KB raw and 84 KB brotli all told — and the browser was
 * downloading every byte of it to strip tags out of a question headline. A respondent on the public
 * survey route paid for it too. The browser has had a parser built in for twenty years; this only
 * reaches for the dependency where there is genuinely no DOM.
 *
 * Both files must agree on behaviour. `html-text.test.ts` runs the same cases through each.
 */

/**
 * Checks if a string contains valid HTML markup
 * @param str - The input string to test
 * @returns true if the string contains valid HTML elements, false otherwise
 */
export const isValidHTML = (str: string): boolean => {
  if (!str) return false;

  try {
    const root = parse(str);
    // Check if there are any element nodes (not just text nodes)
    // nodeType 1 = ELEMENT_NODE
    return root.childNodes.some((node) => Number(node.nodeType) === 1);
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
    try {
      const root = parse(str);
      const textContent = root.textContent;
      return textContent.trim();
    } catch {
      // If parsing fails, treat as plain text
      return str.trim();
    }
  }

  return str.trim();
};
