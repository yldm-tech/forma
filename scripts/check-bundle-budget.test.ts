import { readFileSync } from "node:fs";
import { gzipSync } from "node:zlib";
import { describe, expect, test } from "vitest";
// @ts-expect-error -- plain .mjs script, no type declarations
import { BUDGETS, evaluateBudgets, formatResult, measureGzipBytes } from "./check-bundle-budget.mjs";

const budget = {
  path: "apps/web/public/js/surveys.umd.cjs",
  maxGzipBytes: 300_000,
  measuredBytes: 286_993,
  builtBy: "pnpm build --filter=@forma/surveys",
};

describe("evaluateBudgets", () => {
  test("passes a bundle inside its budget", () => {
    const [result] = evaluateBudgets([budget], () => 286_993);
    expect(result).toMatchObject({ status: "ok", gzipBytes: 286_993 });
  });

  test("fails a bundle one byte over its budget", () => {
    const [result] = evaluateBudgets([budget], () => 300_001);
    expect(result.status).toBe("over");
  });

  test("treats a bundle exactly at its budget as passing", () => {
    const [result] = evaluateBudgets([budget], () => 300_000);
    expect(result.status).toBe("ok");
  });

  // The failure this check exists to prevent: the artifact moves, the walk finds nothing, and the run
  // reports success having measured no bytes at all.
  test("fails rather than passes when the artifact is absent", () => {
    const [result] = evaluateBudgets([budget], () => null);
    expect(result).toMatchObject({ status: "missing", gzipBytes: null });
  });

  test("refuses an empty budget list", () => {
    expect(() => evaluateBudgets([], () => 1)).toThrow(/refusing to report success/);
  });
});

describe("formatResult", () => {
  test("names the build command when the artifact is missing", () => {
    const line = formatResult({ ...budget, gzipBytes: null, status: "missing" });
    expect(line).toContain("pnpm build --filter=@forma/surveys");
  });

  test("reports drift against the committed baseline", () => {
    const line = formatResult({ ...budget, gzipBytes: 287_993, status: "ok" });
    expect(line).toContain("+1,000 B vs baseline");
  });
});

describe("measureGzipBytes", () => {
  test("returns null for a file that does not exist", () => {
    expect(measureGzipBytes("apps/web/public/js/does-not-exist.cjs")).toBeNull();
  });

  test("measures gzip at the pinned level, not the raw size", () => {
    // package.json is committed, so this asserts the compression actually runs without depending on a
    // build artifact that may not be present in the checkout running the suite.
    const source = readFileSync(new URL("../package.json", import.meta.url));
    expect(measureGzipBytes("package.json")).toBe(gzipSync(source, { level: 9 }).length);
  });
});

describe("BUDGETS", () => {
  test("every entry leaves headroom above the size it was baselined at", () => {
    for (const entry of BUDGETS) {
      expect(entry.maxGzipBytes).toBeGreaterThan(entry.measuredBytes);
    }
  });
});
