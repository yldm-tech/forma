#!/usr/bin/env node
// Fails when a committed test file sits outside every workspace, which means nothing runs it.
// Runs as part of `pnpm lint` (root package.json), alongside `catalog:check` and `tenant:check`.
//
// `turbo run test` only invokes `test` in workspaces that define it, so a spec outside every workspace is not skipped loudly — it never runs, and reads as coverage to anyone grepping for one. `scripts/setup-dev-env.test.ts` sat that way for as long as it existed, which is why `scripts/` is now a workspace.
//
// Two kinds of file are expected to be absent from a workspace's own `test` run and are not failures: `*.integration.test.ts`, which `vitest.integration.config.mts` runs against a real Postgres and Redis, and anything under `playwright/`, which is the browser suite.
//
// The check is structural: it asks whether a file lives under a workspace that has a `test` script, not whether that workspace's include globs match it. Asking the stronger question means running `vitest list` in every workspace, which took 54s — a tax on every lint run to catch a case that does not currently exist. As of writing the two answers agree exactly: 894 committed, minus 40 integration and 47 Playwright, is the 807 the workspaces execute. The blind spot is a test placed inside a workspace but excluded by its globs; `pnpm test` reporting a suddenly smaller file count is what would surface that.
import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const REPO_ROOT = fileURLToPath(new URL("..", import.meta.url));

const TEST_SUFFIXES = [".test.ts", ".test.tsx", ".spec.ts", ".spec.tsx"];
const isTestFile = (path) => TEST_SUFFIXES.some((suffix) => path.endsWith(suffix));

/** Absent from a workspace's `test` run by design rather than by accident. */
const RUN_ELSEWHERE = [
  { matches: (path) => path.includes(".integration.test."), why: "vitest.integration.config.mts" },
  { matches: (path) => path.includes("/playwright/"), why: "the Playwright suite" },
];

/** Workspace directories, expanded from the same globs pnpm resolves. */
export const workspaceDirs = (
  globs,
  exists = (p) => existsSync(join(REPO_ROOT, p)),
  list = (p) => readdirSync(join(REPO_ROOT, p))
) => {
  const dirs = new Set();
  for (const glob of globs) {
    if (!glob.endsWith("/*")) {
      dirs.add(glob);
      continue;
    }
    const parent = glob.slice(0, -2);
    if (!exists(parent)) continue;
    for (const entry of list(parent)) dirs.add(`${parent}/${entry}`);
  }
  return [...dirs];
};

export const parseWorkspaceGlobs = (yaml) => {
  const block = /^packages:\s*$([\s\S]*?)(?=^\S)/m.exec(yaml);
  if (!block) throw new Error("pnpm-workspace.yaml has no packages block");
  return [...block[1].matchAll(/^\s*-\s*["']?([^"'\n]+)["']?\s*$/gm)].map((m) => m[1].trim());
};

/** A file is covered when it sits under one of `dirs`. The prefix carries a trailing slash, so `packages/ai` does not claim `packages/ai-extra`. */
export const findOrphans = (testFiles, dirs) => {
  const prefixes = dirs.map((d) => `${d}/`);
  return testFiles.filter(
    (file) => !prefixes.some((p) => file.startsWith(p)) && !RUN_ELSEWHERE.some((r) => r.matches(file))
  );
};

const hasTestScript = (dir) => {
  const manifest = join(REPO_ROOT, dir, "package.json");
  if (!existsSync(manifest)) return false;
  return Boolean(JSON.parse(readFileSync(manifest, "utf8")).scripts?.test);
};

const main = () => {
  const committed = execFileSync("git", ["ls-files"], {
    cwd: REPO_ROOT,
    encoding: "utf8",
    maxBuffer: 64 * 1024 * 1024,
  })
    .split("\n")
    .filter(isTestFile);

  const dirs = workspaceDirs(
    parseWorkspaceGlobs(readFileSync(join(REPO_ROOT, "pnpm-workspace.yaml"), "utf8"))
  ).filter(hasTestScript);

  const orphans = findOrphans(committed, dirs);

  if (orphans.length === 0) {
    console.log(
      `✓ test execution check passed (${committed.length} test files, ${dirs.length} workspaces run tests)`
    );
    return 0;
  }

  console.error(
    `✗ test execution check: ${orphans.length} test file${orphans.length === 1 ? "" : "s"} no workspace runs\n`
  );
  for (const file of orphans) console.error(`  ${file}`);
  console.error(`
A test file outside every workspace is never executed, and nothing says so — it reads as coverage that does not exist.

Move it into a workspace that runs tests, or make its directory one (\`scripts/\` is the worked example). If it is meant to run elsewhere, add that case to RUN_ELSEWHERE here so the exception is written down.
`);
  return 1;
};

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  process.exit(main());
}
