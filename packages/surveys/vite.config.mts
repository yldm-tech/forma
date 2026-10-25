import preact from "@preact/preset-vite";
import { createHash } from "node:crypto";
import { readFileSync, readdirSync } from "node:fs";
import { copyFile, mkdir, rm } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { visualizer } from "rollup-plugin-visualizer";
import { type ConfigEnv, type Plugin, loadEnv, lazyPlugins } from "vite-plus";
import tsconfigPaths from "vite-tsconfig-paths";
import { defineConfig } from "vite-plus";
import { copyCompiledAssetsPlugin } from "../../tooling/vite-plugins/copy-compiled-assets";

// Stubs the @forma/survey-ui/styles?inline import during vitest runs so that
// tests do not require packages/survey-ui to be built first. The plugin only
// activates when VITEST is set, leaving production builds untouched.
const stubSurveyUiStylesForVitest = (): Plugin => {
  const stubId = "\0virtual:survey-ui-styles-stub";
  return {
    name: "forma:stub-survey-ui-styles-in-tests",
    enforce: "pre",
    apply() {
      return process.env.VITEST === "true";
    },
    resolveId(source) {
      if (source === "@forma/survey-ui/styles?inline") {
        return stubId;
      }
      return null;
    },
    load(id) {
      if (id === stubId) {
        return 'export default "";';
      }
      return null;
    },
  };
};

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

// The runtime bundles English (src/lib/i18n.config.ts imports it) and fetches every other language on demand, because a respondent reads one language and the 24 others were about 45 KB of the gzipped bundle. "virtual:survey-locale-files" maps each of those languages to its file, named by a hash of its content so the server can cache it as immutable; the UMD build writes the files beside the runtime, into apps/web/public/js/surveys-locales/, which the Go server serves at /js/surveys-locales/.
const LOCALE_FILES_MODULE = "virtual:survey-locale-files";
const BUNDLED_LOCALE = "en-US";
const LOCALE_FILES_DIR = "surveys-locales";

interface LocaleFile {
  tag: string;
  fileName: string;
  source: string;
}

const readLocaleFiles = (): LocaleFile[] => {
  const localesDir = resolve(__dirname, "locales");
  return readdirSync(localesDir)
    .filter((file) => file.endsWith(".json") && file !== `${BUNDLED_LOCALE}.json`)
    .sort()
    .map((file) => {
      const tag = file.slice(0, -".json".length);
      // Parsed and written again so the file carries no whitespace; the hash is of exactly what is served.
      const source = JSON.stringify(JSON.parse(readFileSync(resolve(localesDir, file), "utf8")));
      const hash = createHash("sha256").update(source).digest("hex").slice(0, 12);
      return { tag, fileName: `${LOCALE_FILES_DIR}/${tag}.${hash}.json`, source };
    });
};

const surveyLocaleFiles = ({ ship }: { ship: boolean }): Plugin => {
  const resolvedId = `\0${LOCALE_FILES_MODULE}`;
  let root = __dirname;
  let emitted: LocaleFile[] = [];
  return {
    name: "forma:survey-locale-files",
    configResolved(config) {
      root = config.root;
    },
    resolveId: (id) => (id === LOCALE_FILES_MODULE ? resolvedId : undefined),
    load(id) {
      if (id !== resolvedId) return undefined;
      const files = readLocaleFiles();
      for (const { tag } of files) this.addWatchFile(resolve(__dirname, "locales", `${tag}.json`));
      return `export default ${JSON.stringify(Object.fromEntries(files.map(({ tag, fileName }) => [tag, fileName])))};`;
    },
    generateBundle() {
      if (!ship) return;
      emitted = readLocaleFiles();
      for (const { fileName, source } of emitted) this.emitFile({ type: "asset", fileName, source });
    },
    // Replaces the folder rather than adding to it, so files of earlier builds, which nothing references any more, are not embedded in the server.
    async writeBundle(options) {
      if (!ship) return;
      const outputDir = resolve(root, "../../apps/web/public/js", LOCALE_FILES_DIR);
      await rm(outputDir, { recursive: true, force: true });
      await mkdir(outputDir, { recursive: true });
      const distDir = options.dir ?? resolve(root, "dist");
      await Promise.all(
        emitted.map(({ fileName }) =>
          copyFile(
            resolve(distDir, fileName),
            resolve(outputDir, fileName.slice(LOCALE_FILES_DIR.length + 1))
          )
        )
      );
    },
  };
};

const config = ({ mode }: ConfigEnv) => {
  const env = loadEnv(mode, process.cwd(), "");

  // Shared configuration
  const sharedConfig = {
    resolve: {
      alias: {
        // Alias React to Preact for survey-ui components
        react: "preact/compat",
        "react-dom": "preact/compat",
        "react/jsx-runtime": "preact/jsx-runtime",
      },
      // The aliases above rewrite survey-ui's `react` imports to `preact/compat`, which Vite then resolves from the importing file, packages/survey-ui/dist, and survey-ui does not depend on Preact. Deduping resolves every `preact` import from this package instead, the one Preact this bundle ships.
      dedupe: ["preact"],
    },
    define: {
      "process.env.NODE_ENV": JSON.stringify(mode),
    },
    plugins: [preact(), tsconfigPaths()],
  };

  // Check if we're building the UMD bundle (separate build step)
  const isUmdBuild = process.env.BUILD_UMD === "true";

  if (isUmdBuild) {
    // UMD build for browser script tag usage (main entry only)
    return defineConfig({
      ...sharedConfig,
      build: {
        emptyOutDir: false,
        lib: {
          entry: resolve(__dirname, "src/index.ts"),
          name: "formaSurveys",
          formats: ["umd"],
          fileName: () => "index.umd.cjs",
        },
        rollupOptions: {
          external: ["node-html-parser"],
        },
        outDir: "dist",
      },
      plugins: lazyPlugins(() => [
        ...sharedConfig.plugins,
        surveyLocaleFiles({ ship: true }),
        copyCompiledAssetsPlugin({
          filename: "surveys",
          distDir: resolve(__dirname, "dist"),
          // See the option's own note: `.cjs` is not on Cloudflare's cacheable-extension list,
          // so the app loads `surveys.umd.js` while embeds keep the `.cjs` name they hardcode.
          duplicateSuffixes: { ".umd.cjs": ".umd.js" },
        }),
      ]),
    });
  }

  // Main ESM build with multiple entry points
  return defineConfig({
    ...sharedConfig,
    test: {
      setupFiles: ["./vitestSetup.ts"],
      exclude: ["dist/**", "node_modules/**"],
      env: env,
      // Environment selection (ENG-1680): Vitest 4 removed `environmentMatchGlobs`, so environments
      // are assigned via projects. *.test.tsx (component tests) run in happy-dom automatically;
      // *.test.ts default to node — the few DOM-dependent .ts tests keep their per-file
      // `@vitest-environment happy-dom` pragma.
      projects: [
        {
          extends: true,
          test: {
            name: "surveys-unit",
            environment: "node",
            include: ["**/*.test.ts"],
            exclude: ["dist/**", "node_modules/**"],
          },
        },
        {
          extends: true,
          test: {
            name: "surveys-components",
            environment: "happy-dom",
            include: ["**/*.test.tsx"],
            exclude: ["dist/**", "node_modules/**"],
          },
        },
      ],
      coverage: {
        provider: "v8",
        reporter: ["text", "html", "lcov"],
        reportsDirectory: "./coverage",
        include: ["src/lib/**/*.ts"],
        exclude: ["**/*.tsx"],
      },
    },
    build: {
      emptyOutDir: false,
      lib: {
        entry: {
          index: resolve(__dirname, "src/index.ts"),
          validation: resolve(__dirname, "src/validation.ts"),
        },
        formats: ["es"],
      },
      rollupOptions: {
        // Externalize node-html-parser to keep bundle size small (~53KB)
        // It's pulled in via @forma/types but not used in browser runtime
        external: ["node-html-parser"],
        output: {
          entryFileNames: "[name].js",
          chunkFileNames: "assets/[name]-[hash].js",
        },
      },
      outDir: "dist",
    },
    plugins: lazyPlugins(() => [
      ...sharedConfig.plugins,
      stubSurveyUiStylesForVitest(),
      surveyLocaleFiles({ ship: false }),
      copyCompiledAssetsPlugin({
        filename: "surveys",
        distDir: resolve(__dirname, "dist"),
        // See the option's own note: `.cjs` is not on Cloudflare's cacheable-extension list,
        // so the app loads `surveys.umd.js` while embeds keep the `.cjs` name they hardcode.
        duplicateSuffixes: { ".umd.cjs": ".umd.js" },
      }),
      process.env.ANALYZE === "true" &&
        visualizer({
          filename: resolve(__dirname, "stats.html"),
          open: false,
          gzipSize: true,
          brotliSize: true,
        }),
    ]),
  });
};

export default config;
