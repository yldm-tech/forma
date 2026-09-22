/// <reference types="vitest" />
import { resolve } from "path";
import dts from "vite-plugin-dts";
import { defineConfig } from "vitest/config";

export default defineConfig({
  resolve: {
    alias: {
      "@": resolve(__dirname, "."),
    },
  },
  build: {
    lib: {
      entry: resolve(__dirname, "src/index.ts"),
      name: "formaJobs",
      fileName: "index",
      formats: ["es", "cjs"],
    },
    rollupOptions: {
      external: ["@forma/logger", "bullmq", "ioredis", "zod"],
    },
    // This package only ever runs in Node - it drives BullMQ workers over ioredis. Without this, vite
    // resolves `node:*` builtins to `__vite-browser-external` and the build fails on the first one
    // imported, which vitest never catches because its own environment is already node.
    ssr: true,
  },
  test: {
    environment: "node",
    globals: true,
    coverage: {
      exclude: ["src/index.ts"],
      reporter: ["text", "json", "html", "lcov"],
    },
  },
  plugins: [
    dts({
      include: ["src/**/*"],
      exclude: ["src/**/*.test.ts"],
      entryRoot: "src",
      outDir: "dist",
    }),
  ],
});
