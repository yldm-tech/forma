import { readFileSync, readdirSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";

/**
 * Both i18next instances in this app run the ICU plugin (`lingodotdev/client.tsx`, `lingodotdev/server.ts`), so the catalogue is ICU MessageFormat, where a placeholder is `{name}`. The i18next default syntax `{{name}}` is a parse error in ICU (`MALFORMED_ARGUMENT`), and i18next-icu's default `parseErrorHandler` returns the raw string rather than throwing — so the braces reach the screen and no value is ever substituted. Nothing else in the pipeline notices: the build passes, the key resolves, the string is simply wrong.
 *
 * Five values had drifted into that syntax before this guard existed — the billing usage card, the API-key permissions label, the copy-survey toast and the two file-size limit strings.
 *
 * This asserts the syntax rather than driving `t()` end to end, because the plugin cannot run here: `i18next-icu` does `import IntlMessageFormat from "intl-messageformat"`, and that package is CJS with `exports.default` set, so a bundler resolves the default correctly while Node's ESM loader hands back the namespace object. Every `t()` call under vitest therefore returns the unformatted source string, correct syntax or not.
 */

const LOCALES_DIR = path.join(import.meta.dirname, "..", "locales");

/**
 * Matches i18next double-brace interpolation and nothing else. An ICU plural nest opens a submessage before its placeholder (`one {{count} apple}`), so the inner argument closes with a single `}` and never matches this.
 */
const I18NEXT_INTERPOLATION = /\{\{\s*[\w.]+\s*\}\}/;

const collectOffendingKeys = (catalogue: unknown, prefix = ""): string[] => {
  if (typeof catalogue === "string") {
    return I18NEXT_INTERPOLATION.test(catalogue) ? [`${prefix} = ${catalogue}`] : [];
  }
  if (catalogue === null || typeof catalogue !== "object") return [];

  return Object.entries(catalogue).flatMap(([key, value]) =>
    collectOffendingKeys(value, prefix ? `${prefix}.${key}` : key)
  );
};

const readCatalogue = (fileName: string): unknown =>
  JSON.parse(readFileSync(path.join(LOCALES_DIR, fileName), "utf8"));

describe("catalogue message syntax", () => {
  test("en-US uses ICU single-brace placeholders, never i18next `{{var}}`", () => {
    // en-US is the only catalogue a human may edit; every other locale is machine-generated from it by Lingo.dev, which copies the source syntax verbatim. So this is where the class is introduced and the only place it can be fixed. The translated catalogues still carry the old braces until `pnpm i18n` regenerates them from this corrected source, which is why they are not asserted on here.
    expect(collectOffendingKeys(readCatalogue("en-US.json"))).toEqual([]);
  });

  test("covers every namespace of the shipped catalogue", () => {
    // Guards the guard: a walk that silently stopped reading the file would pass the assertion above for the wrong reason.
    const namespaces = Object.keys(readCatalogue("en-US.json") as Record<string, unknown>);

    expect(namespaces).toContain("workspace");
    expect(namespaces).toContain("common");
    expect(readdirSync(LOCALES_DIR).filter((file) => file.endsWith(".json"))).toContain("en-US.json");
  });
});
