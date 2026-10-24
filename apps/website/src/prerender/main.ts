// The build's last step (package.json `build`): `vp build` has written the client assets to dist/, this file is then built for Node (`vp build --ssr`) and run, and writes every page as static HTML next to them, plus the 404 pages, sitemap.xml, robots.txt and the nginx redirect rules. It fails the build on invalid content, a page on a reserved path, or an asset over its size budget.
import { mkdirSync, readFileSync, readdirSync, rmSync, statSync, writeFileSync } from "node:fs";
import { dirname, join, relative, sep } from "node:path";
import { fileURLToPath } from "node:url";
import { gzipSync } from "node:zlib";
import type { DocumentAssets } from "../components/Document";
import { validateDictionaries } from "../content";
import { LOCALES, ROUTES, isReservedPath, notFoundPath } from "../site";
import { ROBOTS_TXT, buildRedirectsConf, buildSitemap, notFoundProbePath, renderPath } from "./render";

const ROOT = fileURLToPath(new URL("..", import.meta.url));
const DIST = join(ROOT, "dist");
const PRERENDER = join(ROOT, ".prerender");

const CLIENT_ENTRY = "src/client/main.ts";
const HERO_ENTRY = "src/client/hero-scene.ts";

// Gzip budgets (level 9, as tooling/scripts/check-bundle-budget.mjs measures) from the design spec: the hero chunk is lazy and never on the critical path, everything else is.
const BUDGET = {
  baseJs: 12 * 1024,
  css: 25 * 1024,
  html: 40 * 1024,
  heroJs: 170 * 1024,
};

interface ManifestChunk {
  file: string;
  css?: string[];
  imports?: string[];
  dynamicImports?: string[];
}
type Manifest = Record<string, ManifestChunk>;

const gzipSize = (file: string): number => gzipSync(readFileSync(join(DIST, file)), { level: 9 }).length;

/** A chunk's file and every chunk it imports statically: what loading it costs. */
const staticClosure = (manifest: Manifest, key: string, seen = new Set<string>()): string[] => {
  if (seen.has(key)) return [];
  seen.add(key);
  const chunk = manifest[key];
  if (!chunk) throw new Error(`Vite manifest has no ${key}`);
  return [
    chunk.file,
    ...(chunk.imports ?? []).flatMap((imported) => staticClosure(manifest, imported, seen)),
  ];
};

const write = (urlPath: string, body: string): void => {
  const file = join(DIST, urlPath.endsWith("/") ? `${urlPath}index.html` : urlPath);
  mkdirSync(dirname(file), { recursive: true });
  writeFileSync(file, body);
};

const listFiles = (dir: string): string[] =>
  readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    return statSync(path).isDirectory() ? listFiles(path) : [path];
  });

const main = async (): Promise<void> => {
  validateDictionaries();

  const manifestFile = join(DIST, ".vite", "manifest.json");
  const manifest = JSON.parse(readFileSync(manifestFile, "utf8")) as Manifest;
  const entry = manifest[CLIENT_ENTRY];
  if (!entry) throw new Error(`Vite manifest has no ${CLIENT_ENTRY}`);
  const assets: DocumentAssets = {
    stylesheets: (entry.css ?? []).map((file) => `/${file}`),
    script: `/${entry.file}`,
  };

  const problems: string[] = [];
  const report: string[] = [];
  const measure = (label: string, files: string[], budget: number): void => {
    const size = files.reduce((sum, file) => sum + gzipSize(file), 0);
    report.push(`${label}: ${(size / 1024).toFixed(1)} KB gzip (budget ${budget / 1024} KB)`);
    if (size > budget) problems.push(`${label} is ${size} bytes gzip, over its ${budget}-byte budget`);
  };
  measure("base JS", staticClosure(manifest, CLIENT_ENTRY), BUDGET.baseJs);
  measure("CSS", entry.css ?? [], BUDGET.css);
  if (!entry.dynamicImports?.includes(HERO_ENTRY)) {
    problems.push(`${CLIENT_ENTRY} no longer imports ${HERO_ENTRY} lazily`);
  } else {
    measure("hero chunk (three.js and the scene)", staticClosure(manifest, HERO_ENTRY), BUDGET.heroJs);
  }

  const pages: { path: string; html: string }[] = [];
  for (const route of ROUTES) {
    pages.push({ path: route.path, html: await renderPath(route.path, assets) });
  }
  for (const locale of LOCALES) {
    pages.push({ path: notFoundPath(locale), html: await renderPath(notFoundProbePath(locale), assets) });
  }
  for (const page of pages) {
    const size = gzipSync(page.html, { level: 9 }).length;
    if (size > BUDGET.html)
      problems.push(`${page.path} is ${size} bytes gzip, over the ${BUDGET.html}-byte HTML budget`);
    write(page.path, page.html);
  }
  report.push(`${pages.length} HTML pages`);

  write("/sitemap.xml", buildSitemap());
  write("/robots.txt", ROBOTS_TXT);
  mkdirSync(PRERENDER, { recursive: true });
  writeFileSync(join(PRERENDER, "redirects.conf"), buildRedirectsConf());

  // The manifest has done its job; it is not something to serve.
  rmSync(join(DIST, ".vite"), { recursive: true, force: true });

  const reserved = listFiles(DIST)
    .map((file) => `/${relative(DIST, file).split(sep).join("/")}`)
    .filter(isReservedPath);
  for (const path of reserved) problems.push(`${path} is under a path the app or the server owns`);

  for (const line of report) console.log(`  ${line}`);
  if (problems.length > 0) {
    throw new Error(`Website build failed:\n${problems.map((problem) => `  - ${problem}`).join("\n")}`);
  }
};

await main();
