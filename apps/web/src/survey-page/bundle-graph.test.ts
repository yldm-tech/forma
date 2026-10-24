import { existsSync, readFileSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, test } from "vite-plus/test";

// The public survey page is its own bundle so a respondent downloads as little as possible. Its chunks are named by Rolldown, so a budget on them would break on a bundler upgrade; instead this walks the page's source import graph, which decides what the chunks hold, and holds the two regressions that are easy to reintroduce from a shared module: the admin toaster and recall code reaching the first paint, and every date-fns locale reaching any of the page's screens.

const SRC = dirname(dirname(fileURLToPath(import.meta.url)));
const ENTRY = join(SRC, "survey-page/main.tsx");

const STATIC_IMPORT = /\bimport\s+(type\s+)?[\w*{}\s,$]+?\s+from\s+["']([^"']+)["']/g;
const BARE_IMPORT = /\bimport\s+["']([^"']+)["']/g;
const RE_EXPORT = /\bexport\s+(type\s+)?(?:\{[^}]*\}|\*(?:\s+as\s+\w+)?)\s+from\s+["']([^"']+)["']/g;
const DYNAMIC_IMPORT = /\bimport\(\s*["']([^"']+)["']\s*\)/g;

const resolveRelative = (from: string, specifier: string): string | undefined => {
  const base = join(dirname(from), specifier);
  return [base, `${base}.ts`, `${base}.tsx`, join(base, "index.ts"), join(base, "index.tsx")].find(
    (candidate) => existsSync(candidate) && /\.tsx?$/.test(candidate)
  );
};

type Graph = { files: Set<string>; packages: Set<string> };

/** The modules reachable from the entry, through static imports only or through dynamic ones too. Type-only imports are erased by the build and not followed; packages are recorded by specifier, not entered. */
const reachable = (followDynamic: boolean): Graph => {
  const files = new Set<string>();
  const packages = new Set<string>();
  const queue = [ENTRY];
  for (let file = queue.pop(); file !== undefined; file = queue.pop()) {
    if (files.has(file)) continue;
    files.add(file);
    const source = readFileSync(file, "utf8");
    const specifiers = [
      ...[...source.matchAll(STATIC_IMPORT)].filter((m) => !m[1]).map((m) => m[2]),
      ...[...source.matchAll(BARE_IMPORT)].map((m) => m[1]),
      ...[...source.matchAll(RE_EXPORT)].filter((m) => !m[1]).map((m) => m[2]),
      ...(followDynamic ? [...source.matchAll(DYNAMIC_IMPORT)].map((m) => m[1]) : []),
    ].filter((s): s is string => s !== undefined);
    for (const specifier of specifiers) {
      if (!specifier.startsWith(".")) {
        packages.add(specifier);
        continue;
      }
      const resolved = resolveRelative(file, specifier);
      if (resolved) queue.push(resolved);
    }
  }
  return { files, packages };
};

const names = (graph: Graph) => [...graph.files].map((file) => relative(SRC, file));

describe("the survey page's import graph", () => {
  test("is walked from the real entry", () => {
    // A walk that found nothing would pass every assertion below.
    expect(names(reachable(false))).toEqual(
      expect.arrayContaining(["survey-page/app.tsx", "survey-page/components/survey-client-wrapper.tsx"])
    );
    expect(names(reachable(true))).toContain("survey-page/components/verify-email.tsx");
  });

  test("loads no toaster and no recall code before the first screen", () => {
    // Only the email gate raises toasts and resolves recall; it is a lazy chunk (survey-page/app.tsx).
    expect(names(reachable(false))).not.toEqual(
      expect.arrayContaining([
        expect.stringMatching(/^(components\/ui\/toaster|lib\/toast|lib\/survey\/recall)\.tsx?$/),
      ])
    );
  });

  test("never imports every date-fns locale, on any screen", () => {
    // date-fns/locale is one chunk of every locale the app maps, about 18 KB gzipped; only the admin calendar needs it (lib/survey/date-fns-locale.ts).
    expect([...reachable(true).packages]).not.toContain("date-fns/locale");
  });
});
