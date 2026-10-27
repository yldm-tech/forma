#!/usr/bin/env node
// Fails when a browser bundle every respondent downloads grows past its committed gzip budget.
//
// Three files are in scope: `surveys.umd.cjs` (the survey runtime), `forma.umd.cjs` (the embed SDK) and `embed.js` (the embed script that frames link surveys). They are the whole of what a person answering a survey on a customer's site pays for, the builds of packages/surveys, packages/js-core and packages/embed copy them into `apps/web/public/js/` (the Go server embeds and serves them at `/js/*`), and nothing else watches them: a dependency added three packages away could double the runtime and no check would say so.
//
// So is one page: the public survey page (/s, /c and /p), whose own bundle a respondent of a link survey downloads beside the runtime. Its chunks are named by Rolldown, so a budget on any one of them would break on a bundler upgrade rather than on a regression; the budget is on the page instead, every script, module preload and stylesheet the built `apps/web/dist/survey.html` loads before its first screen, each gzipped on its own as it is served. The rest of the admin SPA is out of scope.
//
// Runs in CI after the build (the TypeScript workspace job in `.github/workflows/pr.yml`), not as part of `pnpm check`: the artifacts exist only after `pnpm build`, and a check on a clean checkout would report both as missing.
//
// A missing file is a failure, never a pass. An artifact-level check whose artifacts moved is exactly the check that reports success having measured nothing, which is the trap `check-tests-run.mjs` exists to close.
import { existsSync, readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { gzipSync } from "node:zlib";

const REPO_ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "../..");

// Pinned rather than left at zlib's default (6), so a budget means the same number on every machine and across Node upgrades. It is not a claim about what a CDN serves — Cloudflare and friends recompress with their own settings — only a stable yardstick for "did this grow".
export const GZIP_LEVEL = 9;

/**
 * The budgets. `maxGzipBytes` is the ceiling; `measuredBytes` is what the file actually was when the
 * budget was last set, and exists so the headroom is visible rather than implied.
 *
 * TO RE-BASELINE: run this script (it prints the current size of every entry, pass or fail), then set
 * `measuredBytes` to what it printed and `maxGzipBytes` to roughly that plus 5%. Do it in the same PR
 * as the change that moved the number, and say in the PR description which way it moved and why. A
 * budget raised in a drive-by commit with no number next to it is how this check becomes noise.
 */
export const BUDGETS = [
  {
    path: "apps/web/public/js/surveys.umd.cjs",
    maxGzipBytes: 234_800,
    measuredBytes: 223_579, // 2026-10-27, after zod left the runtime (the values it read moved into zod-free modules of @forma/types); main measured 250_771 just before
    builtBy: "pnpm --filter @forma/surveys build",
  },
  {
    path: "apps/web/public/js/forma.umd.cjs",
    maxGzipBytes: 13_100,
    measuredBytes: 12_536, // 2026-10-26, after SDK identity verification (setUserId's userHash, survey keys in the user state, setup surviving a refused identify); main measured 12_126 just before
    builtBy: "pnpm --filter @forma/js-core build",
  },
  {
    path: "apps/web/public/js/embed.js",
    maxGzipBytes: 3_200,
    measuredBytes: 3_006, // 2026-10-26, when the embed script was added (standard, popup, slider and side tab)
    builtBy: "pnpm --filter @forma/embed build",
  },
  {
    path: "apps/web/dist/survey.html",
    page: true,
    maxGzipBytes: 161_700,
    measuredBytes: 154_015, // 2026-10-27, when the page got its budget, after zod's schemas left it; main measured about 180_300 just before
    builtBy: "pnpm --filter @forma/web build",
  },
];

// The path the SPA build is served under (APP_BASE_PATH in apps/web/src/lib/base-path.ts); the built HTML names its assets below it.
const APP_BASE = "/app/";

const TAG = /<(script|link)\b([^>]*)>/g;
const ATTRIBUTE = /\b(rel|src|href)="([^"]*)"/g;

/** The asset URLs a built page loads before its first screen: its scripts, module preloads and stylesheets, by the URL the build wrote. Lazy chunks (another screen's, a language's catalogue) are not in the HTML and are not counted. */
export const pageAssets = (html) =>
  [...html.matchAll(TAG)].flatMap(([, tag, attributeText]) => {
    const attributes = Object.fromEntries(
      [...attributeText.matchAll(ATTRIBUTE)].map(([, name, value]) => [name, value])
    );
    if (tag === "script") return attributes.src ? [attributes.src] : [];
    return ["modulepreload", "stylesheet"].includes(attributes.rel) && attributes.href
      ? [attributes.href]
      : [];
  });

/** Gzipped size of a file in bytes, or null when the file is not there. */
export const measureGzipBytes = (relativePath, root = REPO_ROOT) => {
  const absolute = join(root, relativePath);
  if (!existsSync(absolute)) return null;
  return gzipSync(readFileSync(absolute), { level: GZIP_LEVEL }).length;
};

/**
 * Gzipped size of a built page: the sum of its up-front assets (pageAssets), each gzipped on its own as the server sends it, or null when the page or any of its assets is not there. An asset outside the SPA's base path is a failure too: the budget would no longer know what it weighs.
 */
export const measurePageGzipBytes = (relativePath, root = REPO_ROOT) => {
  const absolute = join(root, relativePath);
  if (!existsSync(absolute)) return null;
  const assets = pageAssets(readFileSync(absolute, "utf8"));
  if (assets.length === 0) return null;
  let total = 0;
  for (const url of assets) {
    if (!url.startsWith(APP_BASE)) return null;
    const bytes = measureGzipBytes(join(dirname(relativePath), url.slice(APP_BASE.length)), root);
    if (bytes === null) return null;
    total += bytes;
  }
  return total;
};

/** The measurement a budget entry takes: a page's or a file's. */
export const measureBudget = (budget, root = REPO_ROOT) =>
  budget.page ? measurePageGzipBytes(budget.path, root) : measureGzipBytes(budget.path, root);

/**
 * Compare every budget against a measurement. `measure(path, budget)` returns the gzipped size, or null when the artifact is absent — which is a `missing` result, never an implicit pass.
 */
export const evaluateBudgets = (budgets, measure) => {
  if (budgets.length === 0) {
    throw new Error("check-bundle-budget has no budgets to check — refusing to report success.");
  }
  return budgets.map((budget) => {
    const gzipBytes = measure(budget.path, budget);
    if (gzipBytes === null) return { ...budget, gzipBytes: null, status: "missing" };
    return { ...budget, gzipBytes, status: gzipBytes > budget.maxGzipBytes ? "over" : "ok" };
  });
};

const kb = (bytes) => `${(bytes / 1024).toFixed(1)} KB`;
const percent = (bytes, budget) => `${((bytes / budget) * 100).toFixed(0)}%`;

export const formatResult = (result) => {
  if (result.status === "missing") {
    return `  ✗ ${result.path} — not found. Build it with \`${result.builtBy}\`.`;
  }
  const mark = result.status === "over" ? "✗" : "✓";
  const size = `${kb(result.gzipBytes)} gzip (${result.gzipBytes.toLocaleString("en-US")} B)`;
  const against = `budget ${kb(result.maxGzipBytes)}, ${percent(result.gzipBytes, result.maxGzipBytes)} used`;
  const delta = result.gzipBytes - result.measuredBytes;
  const drift = delta === 0 ? "" : `, ${delta > 0 ? "+" : ""}${delta.toLocaleString("en-US")} B vs baseline`;
  return `  ${mark} ${result.path}: ${size} — ${against}${drift}`;
};

const main = () => {
  const results = evaluateBudgets(BUDGETS, (_path, budget) => measureBudget(budget));
  for (const result of results) console.log(formatResult(result));

  const missing = results.filter((r) => r.status === "missing");
  const over = results.filter((r) => r.status === "over");

  if (missing.length === 0 && over.length === 0) {
    console.log(`✓ bundle budget check passed (${results.length} bundles)`);
    return 0;
  }

  if (missing.length > 0) {
    console.error(`\n✗ Budgeted bundle not built (${missing.length}):\n`);
    for (const result of missing) console.error(`  ${result.path} — \`${result.builtBy}\``);
    console.error(`\n  This check measures build output, so an absent file means it measured nothing rather`);
    console.error(`  than that nothing grew. Build the bundle before running it, or — if the artifact has`);
    console.error(`  genuinely moved — update BUDGETS in tooling/scripts/check-bundle-budget.mjs.`);
  }

  if (over.length > 0) {
    console.error(`\n✗ Bundle over its gzip budget (${over.length}):\n`);
    for (const result of over) {
      const excess = result.gzipBytes - result.maxGzipBytes;
      console.error(
        `  ${result.path}: ${kb(result.gzipBytes)} gzip, ${kb(excess)} over the ${kb(result.maxGzipBytes)} budget`
      );
    }
    console.error(
      `\n  Every byte here is on the critical path of every survey a respondent opens. Find what`
    );
    console.error(
      `  grew before raising the budget: \`ANALYZE=true pnpm --filter @forma/surveys build\` writes`
    );
    console.error(`  packages/surveys/stats.html for the runtime, and the survey page's chunks carry source`);
    console.error(`  maps (apps/web/dist/assets/*.js.map) that name every module in them. A new top-level`);
    console.error(`  import of a locale table, a validation library (zod: import the zod-free modules of`);
    console.error(`  @forma/types, such as card-layout.ts) or a Node-only parser is the usual cause.`);
    console.error(
      `\n  If the growth is intended, re-baseline BUDGETS in tooling/scripts/check-bundle-budget.mjs and`
    );
    console.error(`  say so in the PR description.`);
  }

  console.error("");
  return 1;
};

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  process.exit(main());
}
