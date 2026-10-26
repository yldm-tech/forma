/// <reference types="vite-plus/test" />
import { resolve } from "path";
import { defineConfig, lazyPlugins } from "vite-plus";
import type { ViteUserConfig } from "vite-plus";
import { copyCompiledAssetsPlugin } from "../../tooling/vite-plugins/copy-compiled-assets";

type VitestPluginOption = NonNullable<ViteUserConfig["plugins"]>[number];

export default defineConfig({
  build: {
    lib: {
      entry: resolve(__dirname, "src/index.ts"),
      // Required for an IIFE build; the entry exports nothing, so no global of this name is created.
      name: "formaEmbedScript",
      // A classic script with no exports: customer pages load it with a plain script tag and it defines window.formaEmbed itself.
      formats: ["iife"],
      fileName: () => "index.js",
    },
  },
  plugins: lazyPlugins(() => [
    // dist/index.js becomes apps/web/public/js/embed.js, which the Go server serves at /js/embed.js with the SDK's cache and CORS headers.
    copyCompiledAssetsPlugin({
      filename: "embed",
      distDir: resolve(__dirname, "dist"),
    }) as VitestPluginOption,
  ]),
  test: {
    environment: "jsdom",
    coverage: {
      provider: "v8",
      reporter: ["text", "json", "html", "lcov"],
      reportsDirectory: "./coverage",
      include: ["src/**/*.ts"],
      exclude: ["**/*.test.*"],
    },
  },
});
