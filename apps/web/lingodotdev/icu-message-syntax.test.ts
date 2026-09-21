import { readFileSync, readdirSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";

/**
 * Both i18next instances in this app run the ICU plugin (`client.tsx`, `server.ts`), so the catalogue is
 * ICU, not i18next interpolation: a placeholder is `{name}`, never `{{name}}`. The two syntaxes fail in
 * opposite directions and only one of them is loud. `{{name}}` is a syntax error to ICU, and
 * i18next-icu's default `parseErrorHandler` returns the message untouched rather than throwing, so the
 * string reaches the user with its braces intact — "{{current}} / {{limit}} used" on the billing card —
 * and nothing in the app, the build or the test suite says a word.
 *
 * Registering the ICU plugin also replaces i18next's own interpolation rather than layering on top of it,
 * which is why writing `{{name}}` does not merely bypass ICU and get substituted natively. There is no
 * path on which the doubled form works.
 *
 * The check is deliberately textual rather than a render assertion: under Vitest's default resolution
 * `i18next-icu` is externalised and loads a build whose `IntlMessageFormat` import resolves to a
 * namespace object, so every message fails to parse and a rendering test passes for the wrong reason —
 * it cannot tell a malformed message from a working one. Reading the catalogue avoids that trap
 * entirely.
 */

const LOCALES_DIR = path.join(__dirname, "..", "locales");
// Read the directory rather than `AVAILABLE_LOCALES`, so a catalogue added without being wired into the
// constant is still checked instead of silently skipped.
const CATALOGUES = readdirSync(LOCALES_DIR).filter((file) => file.endsWith(".json"));

/**
 * A doubled brace pair around a plain placeholder name. ICU's own nested sub-messages produce a
 * `{{` sequence too — `{count, plural, one {{count} file} other {{count} files}}` — but never a
 * balanced `{{name}}`, so requiring the closing pair keeps the 24 legitimate plural messages out.
 */
const I18NEXT_PLACEHOLDER = /\{\{\s*[\w.]+\s*\}\}/;

const flatten = (node: unknown, prefix = ""): [string, string][] => {
  if (typeof node === "string") return [[prefix, node]];
  if (!node || typeof node !== "object" || Array.isArray(node)) return [];
  return Object.entries(node).flatMap(([key, value]) => flatten(value, prefix ? `${prefix}.${key}` : key));
};

describe("ICU message syntax", () => {
  // Every catalogue, not just the source. Lingo.dev propagates the source's placeholder syntax, so a
  // doubled brace written in `en-US.json` reaches all four; but it has also introduced the form on its
  // own — `edit_shared_action_warning_description` was `{surveys}` in en-US and `{{surveys}}` in all
  // three translations — so checking the source alone would not have caught it.
  test.each(CATALOGUES)("%s carries no i18next-style {{placeholder}}", (file) => {
    const catalogue: unknown = JSON.parse(readFileSync(path.join(LOCALES_DIR, file), "utf-8"));

    const offenders = flatten(catalogue)
      .filter(([, value]) => I18NEXT_PLACEHOLDER.test(value))
      .map(([key, value]) => `${key}: ${value}`);

    expect(offenders).toEqual([]);
  });

  test("the pattern still recognises the form it exists to reject, and spares ICU plurals", () => {
    expect(I18NEXT_PLACEHOLDER.test("{{current}} / {{limit}} <muted>used</muted>")).toBe(true);
    expect(I18NEXT_PLACEHOLDER.test("View permissions for {{label}}")).toBe(true);

    expect(I18NEXT_PLACEHOLDER.test("{current} / {limit} <muted>used</muted>")).toBe(false);
    expect(I18NEXT_PLACEHOLDER.test("{count, plural, one {{count} file} other {{count} files}}")).toBe(false);
  });
});
