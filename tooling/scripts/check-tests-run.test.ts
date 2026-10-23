import { describe, expect, test } from "vite-plus/test";
import { findOrphans, parseWorkspaceGlobs, workspaceDirs } from "./check-tests-run.mjs";

const YAML = `packages:
  - "apps/*"
  - "docker"
  - "packages/*"
  - "scripts"

allowBuilds:
  esbuild: true
`;

describe("parseWorkspaceGlobs", () => {
  test("reads the globs and stops at the next top-level key", () => {
    expect(parseWorkspaceGlobs(YAML)).toEqual(["apps/*", "docker", "packages/*", "scripts"]);
  });

  test("refuses a file with no packages block rather than reporting everything as an orphan", () => {
    expect(() => parseWorkspaceGlobs("allowBuilds:\n  esbuild: true\n")).toThrow(/packages block/);
  });
});

describe("workspaceDirs", () => {
  const exists = (p: string) => ["apps", "packages"].includes(p);
  const list = (p: string) => (p === "apps" ? ["web", "storybook"] : ["ai", "database"]);

  test("expands a trailing-star glob and keeps a literal entry as-is", () => {
    expect(workspaceDirs(["apps/*", "docker", "packages/*"], exists, list)).toEqual([
      "apps/web",
      "apps/storybook",
      "docker",
      "packages/ai",
      "packages/database",
    ]);
  });

  test("skips a glob whose parent does not exist", () => {
    expect(workspaceDirs(["missing/*"], exists, list)).toEqual([]);
  });
});

describe("findOrphans", () => {
  const dirs = ["apps/web", "packages/ai", "scripts"];

  test("reports a test file outside every workspace", () => {
    expect(findOrphans(["tools/thing.test.ts"], dirs)).toEqual(["tools/thing.test.ts"]);
  });

  test("accepts a test file inside a workspace", () => {
    expect(findOrphans(["apps/web/lib/thing.test.ts", "scripts/x.test.ts"], dirs)).toEqual([]);
  });

  test("does not let one workspace claim a sibling with the same prefix", () => {
    expect(findOrphans(["packages/ai-extra/thing.test.ts"], dirs)).toEqual([
      "packages/ai-extra/thing.test.ts",
    ]);
  });

  test("leaves the Playwright specs under e2e/ alone", () => {
    expect(findOrphans(["e2e/survey.spec.ts", "e2e/accessibility.spec.ts"], dirs)).toEqual([]);
  });

  test("does not exempt a spec that merely mentions e2e deeper in its path", () => {
    expect(findOrphans(["tools/e2e/thing.spec.ts"], dirs)).toEqual(["tools/e2e/thing.spec.ts"]);
  });
});
