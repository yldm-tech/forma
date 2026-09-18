import { mkdir, mkdtemp, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { afterEach, describe, expect, test } from "vitest";
import { copyCompiledAssetsPlugin } from "./index";

/**
 * The plugin writes into `apps/web/public/js` relative to the Vite root it is given, so each case
 * builds a throwaway tree shaped like the monorepo and points the root at `<tmp>/packages/surveys`.
 */
const trees: string[] = [];

const makeTree = async (distFiles: readonly string[]) => {
  const base = await mkdtemp(path.join(tmpdir(), "copy-assets-"));
  trees.push(base);
  const root = path.join(base, "packages", "surveys");
  const dist = path.join(root, "dist");
  const out = path.join(base, "apps", "web", "public", "js");
  await mkdir(dist, { recursive: true });
  await Promise.all(distFiles.map((file) => writeFile(path.join(dist, file), `// ${file}`, "utf8")));
  return { root, dist, out };
};

const run = async (plugin: ReturnType<typeof copyCompiledAssetsPlugin>, root: string): Promise<void> => {
  const hooks = plugin as unknown as {
    configResolved: (config: { root: string }) => void;
    writeBundle: () => Promise<void>;
  };
  hooks.configResolved({ root });
  await hooks.writeBundle();
};

afterEach(async () => {
  await Promise.all(trees.splice(0).map((dir) => rm(dir, { force: true, recursive: true })));
});

describe("copyCompiledAssetsPlugin duplicateSuffixes", () => {
  test("writes the survey bundle under a second, CDN-cacheable extension", async () => {
    // Cloudflare's standard cache level will not cache a `.cjs`, so the app loads `.umd.js` while
    // the embed snippets in packages/js-core keep asking for the `.cjs` name they hardcode. Both
    // have to exist; emitting only one breaks either caching or every embedded survey.
    const { root, dist, out } = await makeTree(["index.umd.cjs", "index.js"]);

    await run(
      copyCompiledAssetsPlugin({
        filename: "surveys",
        distDir: dist,
        duplicateSuffixes: { ".umd.cjs": ".umd.js" },
      }),
      root
    );

    expect((await readdir(out)).sort()).toEqual(["surveys.js", "surveys.umd.cjs", "surveys.umd.js"]);
  });

  test("copies the duplicate's content, not an empty placeholder", async () => {
    const { root, dist, out } = await makeTree(["index.umd.cjs"]);

    await run(
      copyCompiledAssetsPlugin({
        filename: "surveys",
        distDir: dist,
        duplicateSuffixes: { ".umd.cjs": ".umd.js" },
      }),
      root
    );

    const [cjs, js] = await Promise.all([
      readFile(path.join(out, "surveys.umd.cjs"), "utf8"),
      readFile(path.join(out, "surveys.umd.js"), "utf8"),
    ]);
    expect(js).toBe(cjs);
  });

  test("writes nothing extra when no duplicates are configured", async () => {
    const { root, dist, out } = await makeTree(["index.umd.cjs", "index.js"]);

    await run(copyCompiledAssetsPlugin({ filename: "forma", distDir: dist }), root);

    // js-core's embed SDK is loaded by third-party pages under its published name; it must not grow
    // an alias just because the surveys bundle needed one.
    expect((await readdir(out)).sort()).toEqual(["forma.js", "forma.umd.cjs"]);
  });

  test("leaves files that do not match the suffix alone", async () => {
    const { root, dist, out } = await makeTree(["index.js", "index.d.ts"]);

    await run(
      copyCompiledAssetsPlugin({
        filename: "surveys",
        distDir: dist,
        duplicateSuffixes: { ".umd.cjs": ".umd.js" },
      }),
      root
    );

    expect((await readdir(out)).sort()).toEqual(["surveys.d.ts", "surveys.js"]);
  });
});
