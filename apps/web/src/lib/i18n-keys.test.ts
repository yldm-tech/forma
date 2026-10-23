import { expect, test } from "vite-plus/test";
import enUS from "../../locales/en-US.json";
import jaJP from "../../locales/ja-JP.json";
import zhHansCN from "../../locales/zh-Hans-CN.json";
import zhHantTW from "../../locales/zh-Hant-TW.json";

// Every literal key the screens pass to t() must exist in every catalogue, or a screen shows a raw key (or English) in that language.
const sources = import.meta.glob<string>(
  ["../**/*.{ts,tsx}", "!../**/*.test.{ts,tsx}", "!../routeTree.gen.ts"],
  {
    query: "?raw",
    import: "default",
    eager: true,
  }
);

const KEY = /\bt\(\s*"([a-z][\w-]*(?:\.[\w-]+)+)"/g;

// Resolves a key the way i18next does: segment by segment, and, like i18next's deepFind, also through a catalogue key that itself contains dots (forma2 wrote "display_criteria.everyone" beside "display_criteria").
const lookup = (catalogue: unknown, key: string): unknown => {
  const resolve = (node: unknown, parts: string[]): unknown => {
    if (parts.length === 0) return node;
    if (!node || typeof node !== "object") return undefined;
    const record = node as Record<string, unknown>;
    for (let end = 1; end <= parts.length; end++) {
      const segment = parts.slice(0, end).join(".");
      if (!Object.hasOwn(record, segment)) continue;
      const found = resolve(record[segment], parts.slice(end));
      if (found !== undefined) return found;
    }
    return undefined;
  };
  return resolve(catalogue, key.split("."));
};

test("every translation key used by the SPA exists in all four catalogues", () => {
  const keys = new Set<string>();
  for (const source of Object.values(sources)) {
    for (const match of source.matchAll(KEY)) if (match[1]) keys.add(match[1]);
  }
  expect(keys.size).toBeGreaterThan(100);
  const catalogues = { "en-US": enUS, "ja-JP": jaJP, "zh-Hans-CN": zhHansCN, "zh-Hant-TW": zhHantTW };
  const missing = Object.entries(catalogues).flatMap(([locale, catalogue]) =>
    [...keys].filter((key) => typeof lookup(catalogue, key) !== "string").map((key) => `${locale}: ${key}`)
  );
  expect(missing).toEqual([]);
});
