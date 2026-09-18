import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";
import { PUBLIC_I18N_NAMESPACES, SERVER_ONLY_I18N_NAMESPACES } from "./namespaces";

/**
 * The public routes are served the `PUBLIC_I18N_NAMESPACES` slice of the catalogue and nothing else,
 * because the admin namespaces are 199 KB a respondent must never download. That makes the split a
 * correctness claim: a component reachable from a public route that translates a key outside those
 * namespaces renders the raw key to a respondent.
 *
 * So this walks the public surfaces' own module graph and checks every `t("…")` literal it finds
 * against the declared set, which turns the split from a guess into something that goes red when
 * someone reaches for an admin string from public code. It found one when it was written:
 * `link-survey-wrapper` used `workspace.surveys.edit.survey_preview` for the preview banner.
 *
 * Deliberately shallow on one axis — it reads literal keys, not computed ones — and deliberately
 * broad on the other: it follows relative and `@/` imports outward from the entry points rather
 * than guessing which files matter.
 */

const WEB_ROOT = path.join(import.meta.dirname, "..");

/** The routes a respondent can reach without a session. */
const PUBLIC_ENTRY_POINTS = [
  "modules/survey/link/components/survey-renderer.tsx",
  "modules/survey/link/components/survey-client-wrapper.tsx",
  "modules/survey/link/components/pin-screen.tsx",
  "app/not-found.tsx",
];

const EXTENSIONS = [".ts", ".tsx", "/index.ts", "/index.tsx"];

const resolveImport = (specifier: string, fromFile: string): string | null => {
  let base: string;
  if (specifier.startsWith("@/")) {
    base = path.join(WEB_ROOT, specifier.slice(2));
  } else if (specifier.startsWith(".")) {
    base = path.resolve(path.dirname(fromFile), specifier);
  } else {
    // A package, including every `@forma/*` workspace. Their strings are their own concern.
    return null;
  }

  for (const extension of EXTENSIONS) {
    const candidate = base.endsWith(".ts") || base.endsWith(".tsx") ? base : `${base}${extension}`;
    try {
      readFileSync(candidate, "utf8");
      return candidate;
    } catch {
      continue;
    }
  }
  return null;
};

const collectReachableFiles = (entryPoints: readonly string[]): string[] => {
  const seen = new Set<string>();
  const queue = entryPoints.map((entry) => path.join(WEB_ROOT, entry));

  while (queue.length > 0) {
    const file = queue.pop();
    if (!file || seen.has(file)) continue;

    let source: string;
    try {
      source = readFileSync(file, "utf8");
    } catch {
      continue;
    }
    seen.add(file);

    for (const match of source.matchAll(/(?:from\s+|import\s*\(\s*)["']([^"']+)["']/g)) {
      const resolved = resolveImport(match[1], file);
      if (resolved && !seen.has(resolved)) queue.push(resolved);
    }
  }

  return [...seen];
};

/** `t("some.key")` and `t("some.key", { … })`, which is every translation call in this repo. */
const collectTranslationKeys = (file: string): string[] => {
  const source = readFileSync(file, "utf8");
  return [...source.matchAll(/\bt\(\s*["'`]([a-z][a-z0-9_]*(?:\.[A-Za-z0-9_-]+)+)["'`]/g)].map(
    (match) => match[1]
  );
};

describe("public routes only translate against the namespaces they are served", () => {
  const files = collectReachableFiles(PUBLIC_ENTRY_POINTS);

  test("the walk actually reached the public surfaces", () => {
    // Guards the guard: a broken resolver would make the assertion below vacuously true.
    expect(files.length).toBeGreaterThan(30);
    expect(files.some((file) => file.endsWith("link-survey-wrapper.tsx"))).toBe(true);
  });

  test("every literal key belongs to a public namespace", () => {
    // The server-only namespaces are legitimately outside the browser's slice: they are rendered
    // through `getTranslate`, which loads the whole catalogue on the server, and never reach a
    // client component. Widening the allowance to anything else would defeat the test.
    const allowed = new Set<string>([...PUBLIC_I18N_NAMESPACES, ...SERVER_ONLY_I18N_NAMESPACES]);
    const offenders: string[] = [];

    for (const file of files) {
      for (const key of collectTranslationKeys(file)) {
        const namespace = key.split(".")[0];
        if (!allowed.has(namespace)) {
          offenders.push(`${path.relative(WEB_ROOT, file)}: ${key}`);
        }
      }
    }

    expect(offenders).toEqual([]);
  });
});
