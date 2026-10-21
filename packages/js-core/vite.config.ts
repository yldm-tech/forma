/// <reference types="vite-plus/test" />
import { resolve } from "path";
import dts from "vite-plugin-dts";
import { defineConfig, lazyPlugins } from "vite-plus";
import type { ViteUserConfig } from "vite-plus";
import { copyCompiledAssetsPlugin } from "../vite-plugins/copy-compiled-assets";
import { rewriteNodeNextDtsSpecifiers } from "../vite-plugins/node-next-dts";

type VitestPluginOption = NonNullable<ViteUserConfig["plugins"]>[number];

export default defineConfig({
  resolve: {
    alias: {
      "@": resolve(__dirname, "src"),
    },
  },
  build: {
    rollupOptions: {
      output: { inlineDynamicImports: true },
    },
    emptyOutDir: false, // keep the dist folder to avoid errors with pnpm go when folder is empty during build
    lib: {
      entry: resolve(__dirname, "src/index.ts"),
      name: "forma",
      formats: ["umd"],
      fileName: "index",
    },
  },
  plugins: lazyPlugins(() => [
    dts({
      rollupTypes: false,
      insertTypesEntry: true,
      include: ["src/**/*.ts"],
      exclude: ["src/**/*.test.ts", "src/**/*.spec.ts", "src/**/tests/**", "vitest.setup.ts"],
      beforeWriteFile: rewriteNodeNextDtsSpecifiers,
    }) as VitestPluginOption,
    copyCompiledAssetsPlugin({
      filename: "forma",
      distDir: resolve(__dirname, "dist"),
      skipDirectoryCheck: true, // Skip checking for subdirectories that might not exist
    }) as VitestPluginOption,
  ]),
  test: {
    // Vitest v4 compatibility: preserve mock call history.
    // Remove after tests no longer rely on calls from setup or earlier tests.
    // https://viteplus.dev/guide/vitest-v5#remove-unneeded-compatibility-settings
    // https://vitest.dev/guide/migration/#clearmocks-is-enabled-by-default
    clearMocks: false,
    environment: "node",
    setupFiles: ["./vitest.setup.ts"],
    coverage: {
      provider: "v8",
      reporter: ["text", "json", "html", "lcov"],
      reportsDirectory: "./coverage",
      include: ["src/lib/**/*.ts"],
      exclude: ["src/lib/**/*.mock.ts", "vitest.setup.ts", "**/*.test.*", "**/*.spec.*"],
    },
  },
});
