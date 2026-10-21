import tailwindcss from "@tailwindcss/vite";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import { defineConfig, lazyPlugins } from "vite-plus";

// In development the Go server runs on :3000 and Vite proxies every server path to it, so the SPA talks to the API same-origin exactly as it does in production, where the Go binary serves the built files itself.
const goServer = process.env.FORMA_SERVER_URL ?? "http://localhost:3000";

export default defineConfig({
  plugins: lazyPlugins(() => [
    tanstackRouter({ target: "react", autoCodeSplitting: true }),
    react(),
    tailwindcss(),
  ]),
  server: {
    port: 5173,
    proxy: {
      "/api": goServer,
      "/health": goServer,
    },
  },
  build: {
    outDir: "dist",
    sourcemap: true,
  },
  test: {
    // Vitest v4 compatibility: preserve mock call history.
    // Remove after tests no longer rely on calls from setup or earlier tests.
    // https://viteplus.dev/guide/vitest-v5#remove-unneeded-compatibility-settings
    // https://vitest.dev/guide/migration/#clearmocks-is-enabled-by-default
    clearMocks: false,
    environment: "node",
    include: ["src/**/*.test.{ts,tsx}"],
    // i18next-icu default-imports intl-messageformat, whose Node entry is CommonJS: externalized, the import is the module object rather than the class and every ICU message comes back unformatted. Transforming both resolves the ESM build, as the browser bundle does.
    server: { deps: { inline: ["i18next-icu", "intl-messageformat"] } },
  },
});
