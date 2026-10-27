import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { gzipSync } from "node:zlib";
import { describe, expect, test } from "vite-plus/test";
import {
  BUDGETS,
  evaluateBudgets,
  formatResult,
  measureBudget,
  measureGzipBytes,
  measurePageGzipBytes,
  pageAssets,
} from "./check-bundle-budget.mjs";

const budget = {
  path: "apps/web/public/js/surveys.umd.cjs",
  maxGzipBytes: 300_000,
  measuredBytes: 286_993,
  builtBy: "pnpm --filter @forma/surveys build",
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
    expect(line).toContain("pnpm --filter @forma/surveys build");
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
    const source = readFileSync(new URL("../../package.json", import.meta.url));
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

describe("measurePageGzipBytes", () => {
  // A built page in a scratch root: survey.html names its assets under the SPA's base path, as Vite writes them.
  const builtPage = (html: string, assets: Record<string, string>) => {
    const root = mkdtempSync(join(tmpdir(), "bundle-budget-"));
    mkdirSync(join(root, "apps/web/dist/assets"), { recursive: true });
    writeFileSync(join(root, "apps/web/dist/survey.html"), html);
    for (const [name, content] of Object.entries(assets)) {
      writeFileSync(join(root, "apps/web/dist/assets", name), content);
    }
    return root;
  };
  const gzip = (content: string) => gzipSync(Buffer.from(content), { level: 9 }).length;
  const page = `<!doctype html><html><head>
    <script type="module" crossorigin src="/app/assets/survey-a.js"></script>
    <link rel="modulepreload" crossorigin href="/app/assets/react-b.js">
    <link crossorigin href="/app/assets/logo-c.js" rel="modulepreload">
    <link rel="stylesheet" crossorigin href="/app/assets/survey-d.css">
    <link rel="icon" href="/app/favicon.ico">
  </head><body><div id="root"></div></body></html>`;
  const assets = {
    "survey-a.js": "entry();",
    "react-b.js": "react();",
    "logo-c.js": "logo();",
    "survey-d.css": "a{}",
  };

  test("lists every script, module preload and stylesheet, whatever the order of their attributes", () => {
    expect(pageAssets(page)).toEqual([
      "/app/assets/survey-a.js",
      "/app/assets/react-b.js",
      "/app/assets/logo-c.js",
      "/app/assets/survey-d.css",
    ]);
  });

  test("weighs the page as the sum of its assets, each gzipped on its own", () => {
    const root = builtPage(page, assets);
    try {
      const expected = Object.values(assets).reduce((sum, content) => sum + gzip(content), 0);
      expect(measurePageGzipBytes("apps/web/dist/survey.html", root)).toBe(expected);
      expect(measureBudget({ path: "apps/web/dist/survey.html", page: true }, root)).toBe(expected);
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  // As for a file: a page that moved, or one whose assets did, must not pass having weighed nothing.
  test("is missing when the page, one of its assets, or every asset is not there", () => {
    const withoutLogo = Object.fromEntries(Object.entries(assets).filter(([name]) => name !== "logo-c.js"));
    const roots = [builtPage(page, withoutLogo), builtPage("<!doctype html><html></html>", assets)];
    try {
      for (const root of roots) {
        expect(measurePageGzipBytes("apps/web/dist/survey.html", root)).toBeNull();
      }
      expect(measurePageGzipBytes("apps/web/dist/missing.html", roots[0])).toBeNull();
    } finally {
      for (const root of roots) rmSync(root, { recursive: true, force: true });
    }
  });

  test("is missing when an asset is outside the SPA's base path", () => {
    const root = builtPage(
      page.replace("/app/assets/react-b.js", "https://cdn.example.com/react-b.js"),
      assets
    );
    try {
      expect(measurePageGzipBytes("apps/web/dist/survey.html", root)).toBeNull();
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });
});
