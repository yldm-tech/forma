/// <reference types="vite-plus/test" />
import { resolve } from "node:path";
import { defineConfig } from "vite-plus";

export default defineConfig({
  build: {
    // One entry per public subpath (ENG-2306). `canonical` and `utils` are entries in their own
    // right so the bare `.` import and the named subpaths resolve to the same chunks — previously
    // `./src/*` handed out the sources alongside `dist/index.js`, so a consumer reaching both
    // carried two copies of the ISO-639 table.
    lib: {
      entry: {
        index: resolve(__dirname, "src/index.ts"),
        canonical: resolve(__dirname, "src/canonical.ts"),
        "survey-runtime-languages": resolve(__dirname, "src/survey-runtime-languages.ts"),
        utils: resolve(__dirname, "src/utils.ts"),
      },
      formats: ["es", "cjs"],
      fileName: (format, entryName) => `${entryName}.${format === "es" ? "js" : "cjs"}`,
    },
    outDir: "dist",
    emptyOutDir: false,
  },
  test: {
    // Vitest v4 compatibility: preserve mock call history.
    // Remove after tests no longer rely on calls from setup or earlier tests.
    // https://viteplus.dev/guide/vitest-v5#remove-unneeded-compatibility-settings
    // https://vitest.dev/guide/migration/#clearmocks-is-enabled-by-default
    clearMocks: false,
    environment: "node",
    globals: true,
    coverage: {
      reporter: ["text", "json", "html", "lcov"],
    },
  },
});
