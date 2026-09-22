#!/usr/bin/env node
// Fails when a file under `apps/web/modules/` imports from `apps/web/app/` and is not in the committed baseline, and when a baseline entry no longer does — the list may only shrink.
//
// AGENTS.md states the layering rule twice ("`app` and `modules` depend on `lib`, never the reverse", and "Do not add new ones" for `modules/` → `app/`), and `apps/web/eslint.config.mjs` enforces only the `lib/` half. The `modules/` half was prose plus a count, and the count rotted: AGENTS.md said 35, the ESLint comment said 67, and the real number is the 24 production files in the baseline beside this script. A number in prose has no owner; a baseline file has CI.
//
// Division of labour with ESLint: `apps/web/eslint.config.mjs` reads the same baseline and reports a *new* violation at the offending line, which is where a contributor wants to see it. What ESLint cannot see is an entry that has become unnecessary — an `ignores` glob for a file that no longer violates is silently kept forever. That is this script's job, and it is what makes the list a ratchet rather than a second place for the count to rot.
//
// Regenerate with `node scripts/check-modules-app-imports.mjs --update`. It prunes entries that no longer violate; it will not add new ones, because adding one is the thing the check exists to stop. Inverting a dependency is the fix — a route component several modules need belongs in `modules/`, and a module reaching into a route's `lib/` wants the dependency inverted.
//
// Scope, deliberately: static `import`/`export … from` specifiers in committed `.ts`/`.tsx` files. A dynamic `import()` or a re-export through a barrel slips past, the same trade-off `check-tenant-scoping.mjs` makes. This is a ratchet, not a boundary.
import { execFileSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const REPO_ROOT = fileURLToPath(new URL("..", import.meta.url));

export const SCRIPT_PATH = "scripts/check-modules-app-imports.mjs";
export const BASELINE_PATH = "scripts/modules-app-imports-baseline.json";
export const MODULES_DIR = "apps/web/modules/";

/** `from "@/app/…"`, `from "@/app"`, and the side-effect `import "@/app/…"`. */
const APP_IMPORT = /(?:\bfrom\s*|^\s*import\s+)["']@\/app(?:\/[^"']*)?["']/m;

/** Specs are exempt: reaching for a fixture beside the route it was written for is not production code depending upward. Mirrors the `lib/` block's own exemption. */
export const isSpec = (path) => /\.test\.tsx?$/.test(path) || path.includes("/__mocks__/");

export const isModuleSource = (path) => path.startsWith(MODULES_DIR) && /\.tsx?$/.test(path) && !isSpec(path);

export const importsFromApp = (source) => APP_IMPORT.test(source);

/** Files under `modules/` whose source imports from `app/`, sorted. `read` is injected so the diff logic is testable without a filesystem. */
export const scanViolations = (files, read) =>
  files
    .filter(isModuleSource)
    .filter((file) => importsFromApp(read(file)))
    .sort();

/** `added` are violations nobody signed off on; `stale` are baseline entries that no longer violate and must be dropped so the list can only shrink. */
export const diffBaseline = (violations, baseline) => ({
  added: violations.filter((file) => !baseline.includes(file)),
  stale: baseline.filter((file) => !violations.includes(file)),
});

export const readBaseline = (root = REPO_ROOT) =>
  JSON.parse(readFileSync(join(root, BASELINE_PATH), "utf8")).files;

const gitFiles = () =>
  execFileSync("git", ["ls-files", MODULES_DIR], {
    cwd: REPO_ROOT,
    encoding: "utf8",
    maxBuffer: 64 * 1024 * 1024,
  })
    .split("\n")
    .filter(Boolean);

// Keys are emitted in the order `prettier-plugin-sort-json` wants them, so a regenerated baseline is already `pnpm format:check`-clean.
const writeBaseline = (files) => {
  const contents = {
    files,
    regenerate: `node ${SCRIPT_PATH} --update`,
    why: "Files under apps/web/modules/ that still import from apps/web/app/. This list may only shrink — see the script for why, and invert the dependency rather than adding a line here. apps/web/eslint.config.mjs reads it as the `ignores` of its modules/ layering rule.",
  };
  writeFileSync(join(REPO_ROOT, BASELINE_PATH), `${JSON.stringify(contents, null, 2)}\n`);
};

const main = () => {
  const violations = scanViolations(gitFiles(), (file) => readFileSync(join(REPO_ROOT, file), "utf8"));
  const baseline = readBaseline();
  const { added, stale } = diffBaseline(violations, baseline);
  const updating = process.argv.includes("--update");

  if (added.length > 0) {
    console.error(
      `✗ modules/ layering check: ${added.length} new import${added.length === 1 ? "" : "s"} from app/\n`
    );
    for (const file of added) console.error(`  ${file}`);
    console.error(`
\`modules/\` must not import from \`app/\`. A route component several modules need belongs in \`modules/\`; a module reaching into a route's \`lib/\` wants the dependency inverted.

${BASELINE_PATH} is the backlog of files that still do it, and it may only shrink — \`--update\` will not add to it.
`);
    return 1;
  }

  if (stale.length > 0) {
    if (updating) {
      writeBaseline(violations);
      console.log(`✓ pruned ${stale.length} entr${stale.length === 1 ? "y" : "ies"} from ${BASELINE_PATH}`);
      return 0;
    }
    console.error(
      `✗ modules/ layering check: ${stale.length} stale baseline entr${stale.length === 1 ? "y" : "ies"}\n`
    );
    for (const file of stale) console.error(`  ${file}`);
    console.error(`
These no longer import from \`app/\` (or no longer exist), so the baseline is claiming a backlog that is already paid off.

Run \`node ${SCRIPT_PATH} --update\` to prune them.
`);
    return 1;
  }

  if (updating) writeBaseline(violations);
  console.log(`✓ modules/ layering check passed (${violations.length} files still import from app/)`);
  return 0;
};

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  process.exit(main());
}
