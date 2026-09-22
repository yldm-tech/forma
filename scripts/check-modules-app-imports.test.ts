import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, test } from "vitest";
// @ts-expect-error -- plain .mjs script, no type declarations
import {
  BASELINE_PATH,
  MODULES_DIR,
  diffBaseline,
  importsFromApp,
  isModuleSource,
  isSpec,
  readBaseline,
  scanViolations,
} from "./check-modules-app-imports.mjs";

const REPO_ROOT = fileURLToPath(new URL("..", import.meta.url));

describe("importsFromApp", () => {
  test("matches the spellings the layering rule restricts", () => {
    expect(importsFromApp('import { x } from "@/app/lib/thing";')).toBe(true);
    expect(importsFromApp('export { x } from "@/app/lib/thing";')).toBe(true);
    expect(importsFromApp('import type { T } from "@/app";')).toBe(true);
    expect(importsFromApp('import "@/app/globals.css";')).toBe(true);
    expect(importsFromApp('import {\n  x,\n} from "@/app/lib/thing";')).toBe(true);
  });

  test("does not claim a sibling alias that merely starts with app", () => {
    expect(importsFromApp('import { x } from "@/apple/thing";')).toBe(false);
    expect(importsFromApp('import { x } from "@/lib/app/thing";')).toBe(false);
    expect(importsFromApp('const s = "@/app/lib/thing";')).toBe(false);
  });
});

describe("isSpec / isModuleSource", () => {
  test("treats specs and mocks as exempt, the way the lib/ rule does", () => {
    expect(isSpec("apps/web/modules/mcp/tools/surveys.test.ts")).toBe(true);
    expect(isSpec("apps/web/modules/auth/lib/x.integration.test.ts")).toBe(true);
    expect(isSpec("apps/web/modules/x/__mocks__/y.ts")).toBe(true);
    expect(isSpec("apps/web/modules/mcp/tools/surveys.ts")).toBe(false);
  });

  test("covers only .ts/.tsx production files under modules/", () => {
    expect(isModuleSource("apps/web/modules/mcp/auth.ts")).toBe(true);
    expect(isModuleSource("apps/web/modules/settings/components/shell.tsx")).toBe(true);
    expect(isModuleSource("apps/web/modules/mcp/auth.test.ts")).toBe(false);
    expect(isModuleSource("apps/web/lib/survey/service.ts")).toBe(false);
    expect(isModuleSource("apps/web/modules/x/README.md")).toBe(false);
  });
});

describe("scanViolations", () => {
  const read = (file: string) =>
    ({
      "apps/web/modules/b/violates.ts": 'import { x } from "@/app/lib/x";',
      "apps/web/modules/a/clean.ts": 'import { x } from "@/lib/x";',
      "apps/web/modules/a/violates.tsx": 'export { x } from "@/app/(app)/page";',
      "apps/web/modules/a/violates.test.ts": 'import { x } from "@/app/lib/x";',
      "apps/web/lib/outside.ts": 'import { x } from "@/app/lib/x";',
    })[file] ?? "";

  test("reports module sources that import from app/, sorted, specs excluded", () => {
    expect(
      scanViolations(
        [
          "apps/web/modules/b/violates.ts",
          "apps/web/modules/a/clean.ts",
          "apps/web/modules/a/violates.tsx",
          "apps/web/modules/a/violates.test.ts",
          "apps/web/lib/outside.ts",
        ],
        read
      )
    ).toEqual(["apps/web/modules/a/violates.tsx", "apps/web/modules/b/violates.ts"]);
  });
});

describe("diffBaseline", () => {
  test("a violation nobody signed off on is added", () => {
    expect(diffBaseline(["a.ts", "b.ts"], ["a.ts"])).toEqual({ added: ["b.ts"], stale: [] });
  });

  test("a baseline entry that no longer violates is stale, so the list can only shrink", () => {
    expect(diffBaseline(["a.ts"], ["a.ts", "b.ts"])).toEqual({ added: [], stale: ["b.ts"] });
  });

  test("an unchanged backlog is neither", () => {
    expect(diffBaseline(["a.ts"], ["a.ts"])).toEqual({ added: [], stale: [] });
  });
});

// The hookup: `pnpm lint` cannot run this script without a root package.json entry, so the ratchet is held here instead — `turbo run test` runs the `scripts` workspace, and this case fails on both a new violation and a stale entry.
describe("the committed baseline", () => {
  test("matches what modules/ actually imports", () => {
    const files = execFileSync("git", ["ls-files", MODULES_DIR], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      maxBuffer: 64 * 1024 * 1024,
    })
      .split("\n")
      .filter(Boolean);
    const violations = scanViolations(files, (file: string) => readFileSync(join(REPO_ROOT, file), "utf8"));

    expect(diffBaseline(violations, readBaseline(REPO_ROOT))).toEqual({ added: [], stale: [] });
  });

  test("is the only place the count lives, and it may only shrink", () => {
    expect(readBaseline(REPO_ROOT).length).toBeLessThanOrEqual(24);
    expect(BASELINE_PATH).toBe("scripts/modules-app-imports-baseline.json");
  });
});
