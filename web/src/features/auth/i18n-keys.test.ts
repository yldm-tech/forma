import { describe, expect, test } from "vite-plus/test";
import { AVAILABLE_LOCALES } from "../../lib/i18n";

// forma2 failed `scan-translations` on a key missing from a catalogue; the auth screens are checked the same way here. Keys are written as literals (never assembled), so reading the sources finds every one.
const sources = {
  ...import.meta.glob<string>(["./**/*.{ts,tsx}", "!./**/*.test.ts"], {
    query: "?raw",
    import: "default",
    eager: true,
  }),
  ...import.meta.glob<string>(
    [
      "../../routes/auth/**/*.tsx",
      "../../routes/invite.tsx",
      "../../routes/verify-email-change.tsx",
      "../../routes/email-change-without-verification-success.tsx",
    ],
    { query: "?raw", import: "default", eager: true }
  ),
};

const catalogues = import.meta.glob<Record<string, unknown>>("../../../locales/*.json", {
  import: "default",
  eager: true,
});

const KEY = /\bt\(\s*"([\w.-]+)"|key: "(auth\.[\w.-]+)"/g;

const usedKeys = [
  ...new Set(
    Object.values(sources).flatMap((source) =>
      [...source.matchAll(KEY)].map((match) => match[1] ?? match[2] ?? "")
    )
  ),
].sort();

const lookup = (catalogue: Record<string, unknown>, key: string): unknown =>
  key
    .split(".")
    .reduce<unknown>((node, part) => (node as Record<string, unknown> | undefined)?.[part], catalogue);

test("the auth screens use translated text", () => {
  expect(usedKeys.length).toBeGreaterThan(50);
});

describe.each(AVAILABLE_LOCALES)("%s", (locale) => {
  test("has every key the auth screens use", () => {
    const catalogue = catalogues[`../../../locales/${locale}.json`];
    expect(catalogue).toBeDefined();
    const missing = usedKeys.filter((key) => typeof lookup(catalogue ?? {}, key) !== "string");
    expect(missing).toEqual([]);
  });
});
