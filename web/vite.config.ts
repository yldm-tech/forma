import tailwindcss from "@tailwindcss/vite";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { type Plugin, defineConfig, lazyPlugins } from "vite-plus";

// The public survey page loads only the namespaces of a catalogue it reads; the admin app's would be most of the download. "virtual:survey-page-locale/<locale>" is those namespaces of web/locales/<locale>.json.
const SURVEY_PAGE_LOCALE = "virtual:survey-page-locale/";
const SURVEY_PAGE_NAMESPACES = ["c", "common", "s"];

const surveyPageLocales = (): Plugin => ({
  name: "forma-survey-page-locales",
  resolveId: (id) => (id.startsWith(SURVEY_PAGE_LOCALE) ? `\0${id}` : undefined),
  load(id) {
    if (!id.startsWith(`\0${SURVEY_PAGE_LOCALE}`)) return undefined;
    const locale = id.slice(SURVEY_PAGE_LOCALE.length + 1);
    if (!/^[A-Za-z]+(-[A-Za-z]+)*$/.test(locale)) throw new Error(`Not a locale: ${locale}`);
    const file = fileURLToPath(new URL(`locales/${locale}.json`, import.meta.url));
    this.addWatchFile(file);
    const catalogue = JSON.parse(readFileSync(file, "utf8")) as Record<string, unknown>;
    return SURVEY_PAGE_NAMESPACES.map(
      (ns) => `export const ${ns} = ${JSON.stringify(catalogue[ns] ?? {})};`
    ).join("\n");
  },
});

// In development the Go server runs on :3000 and Vite proxies every server path to it, so the SPA talks to the API same-origin exactly as it does in production, where the Go binary serves the built files itself.
const goServer = process.env.FORMA_SERVER_URL ?? "http://localhost:3000";

export default defineConfig({
  plugins: lazyPlugins(() => [
    tanstackRouter({ target: "react", autoCodeSplitting: true }),
    react(),
    tailwindcss(),
    surveyPageLocales(),
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
    rollupOptions: {
      // The admin SPA, and the public survey page as a bundle of its own: respondents never download the admin app. The Go server fills survey.html's markers (server/internal/linksurvey).
      input: {
        main: fileURLToPath(new URL("index.html", import.meta.url)),
        survey: fileURLToPath(new URL("survey.html", import.meta.url)),
      },
    },
  },
  test: {
    // Vitest v4 compatibility: preserve mock call history.
    // Remove after tests no longer rely on calls from setup or earlier tests.
    // https://viteplus.dev/guide/vitest-v5#remove-unneeded-compatibility-settings
    // https://vitest.dev/guide/migration/#clearmocks-is-enabled-by-default
    clearMocks: false,
    // Component tests (*.test.tsx) render into a DOM; everything else runs in Node, where a file that needs a DOM asks for one with a @vitest-environment comment.
    projects: [
      { extends: true, test: { name: "node", environment: "node", include: ["src/**/*.test.ts"] } },
      { extends: true, test: { name: "dom", environment: "jsdom", include: ["src/**/*.test.tsx"] } },
    ],
    // Vitest stubs stylesheets out by default; the rich-text editor's test reads its stylesheet's source, so that one goes through Vite's pipeline.
    css: { include: [/styles-editor-frontend\.css/] },
    // i18next-icu default-imports intl-messageformat, whose Node entry is CommonJS: externalized, the import is the module object rather than the class and every ICU message comes back unformatted. Transforming both resolves the ESM build, as the browser bundle does.
    server: { deps: { inline: ["i18next-icu", "intl-messageformat"] } },
  },
});
