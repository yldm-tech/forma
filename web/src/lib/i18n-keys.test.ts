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

const lookup = (catalogue: unknown, key: string): unknown =>
  key
    .split(".")
    .reduce<unknown>(
      (node, part) =>
        node && typeof node === "object" ? (node as Record<string, unknown>)[part] : undefined,
      catalogue
    );

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
