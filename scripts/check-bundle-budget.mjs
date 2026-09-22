#!/usr/bin/env node
// Fails when a browser bundle every respondent downloads grows past its committed gzip budget.
//
// Two files are in scope, and only two: `surveys.umd.cjs` (the survey runtime) and `forma.umd.cjs` (the embed SDK). They are the whole of what a person answering a survey pays for, they are built by Vite into `apps/web/public/js/`, and nothing else watched them — a dependency added three packages away could double the runtime and no check would say so. Route chunks and the Next.js client manifest are deliberately out of scope: their names and shape are Turbopack-internal, so a budget on them would break on a Next major rather than on a regression.
//
// Runs in CI after the build, not as part of `pnpm lint`: the artifacts exist only after `pnpm build`, and a lint run on a clean checkout would report both as missing. See `.github/workflows/api-v3-contract-tests.yml`, which builds the app on every PR anyway.
//
// A missing file is a failure, never a pass. An artifact-level check whose artifacts moved is exactly the check that reports success having measured nothing, which is the trap `check-tests-run.mjs` exists to close.
import { existsSync, readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { gzipSync } from "node:zlib";

const REPO_ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");

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
    maxGzipBytes: 300_000,
    measuredBytes: 286_993, // 2026-09-22
    builtBy: "pnpm build --filter=@forma/surveys",
  },
  {
    path: "apps/web/public/js/forma.umd.cjs",
    maxGzipBytes: 12_000,
    measuredBytes: 10_859, // 2026-09-22
    builtBy: "pnpm build --filter=@forma/js-core",
  },
];

/** Gzipped size of a file in bytes, or null when the file is not there. */
export const measureGzipBytes = (relativePath, root = REPO_ROOT) => {
  const absolute = join(root, relativePath);
  if (!existsSync(absolute)) return null;
  return gzipSync(readFileSync(absolute), { level: GZIP_LEVEL }).length;
};

/**
 * Compare every budget against a measurement. `measure` returns the gzipped size, or null when the
 * artifact is absent — which is a `missing` result, never an implicit pass.
 */
export const evaluateBudgets = (budgets, measure) => {
  if (budgets.length === 0) {
    throw new Error("check-bundle-budget has no budgets to check — refusing to report success.");
  }
  return budgets.map((budget) => {
    const gzipBytes = measure(budget.path);
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
  const results = evaluateBudgets(BUDGETS, (path) => measureGzipBytes(path));
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
    console.error(`  genuinely moved — update BUDGETS in scripts/check-bundle-budget.mjs.`);
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
      `  grew before raising the budget: \`ANALYZE=true pnpm build --filter=@forma/surveys\` writes`
    );
    console.error(`  packages/surveys/stats.html. A new top-level import of a locale table, a validation`);
    console.error(`  library or a Node-only parser is the usual cause.`);
    console.error(
      `\n  If the growth is intended, re-baseline BUDGETS in scripts/check-bundle-budget.mjs and`
    );
    console.error(`  say so in the PR description.`);
  }

  console.error("");
  return 1;
};

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  process.exit(main());
}
