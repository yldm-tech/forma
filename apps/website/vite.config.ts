import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { type Plugin, defineConfig, lazyPlugins } from "vite-plus";

// The marketing site: React components rendered to static HTML at build time, one page per route and locale (src/prerender/main.ts), with a small client script (src/client/main.ts) as the only JavaScript a page loads. `vp build` writes the client assets; package.json's build script then builds and runs the prerender step.

// SIGNUP_OPEN=1 when forma.yldm.ai accepts public sign-ups (IS_FORMA_CLOUD=1 on the server). Strict like the server's own switches: anything but unset, 0 or 1 fails the build.
const signupOpen = (() => {
  const value = process.env.SIGNUP_OPEN ?? "0";
  if (value !== "0" && value !== "1")
    throw new Error(`SIGNUP_OPEN must be 0 or 1, not ${JSON.stringify(value)}`);
  return value === "1";
})();

// `vp dev`: renders each request through the same router and components the build uses, with Vite serving the client script and stylesheet from source.
const devPages = (): Plugin => ({
  name: "forma-website-dev-pages",
  apply: "serve",
  configureServer(server) {
    return () => {
      server.middlewares.use((req, res, next) => {
        const url = new URL(req.url ?? "/", "http://localhost");
        if (req.method !== "GET" || /\.[a-z0-9]+$/i.test(url.pathname)) return next();
        void (async () => {
          const { renderPath } = (await server.ssrLoadModule(
            "/src/prerender/render.tsx"
          )) as typeof import("./src/prerender/render");
          const html = await renderPath(url.pathname, { stylesheets: [], script: "/src/client/main.ts" });
          res.setHeader("Content-Type", "text/html; charset=utf-8");
          res.end(await server.transformIndexHtml(url.pathname, html));
        })().catch(next);
      });
    };
  },
});

export default defineConfig(({ isSsrBuild }) => ({
  plugins: lazyPlugins(() => [react(), tailwindcss(), devPages()]),
  define: { __SIGNUP_OPEN__: JSON.stringify(signupOpen) },
  appType: "custom",
  server: { port: 5174 },
  build: isSsrBuild
    ? { outDir: ".prerender", emptyOutDir: true, copyPublicDir: false, target: "node22" }
    : {
        outDir: "dist",
        emptyOutDir: true,
        // Everything the build emits sits under /_site/, the one prefix the site claims besides its pages (src/site.ts RESERVED_PREFIXES lists the server's).
        assetsDir: "_site/assets",
        manifest: true,
        // The hero chunk (three.js) is lazy and has its own gzip budget, which src/prerender/main.ts enforces.
        chunkSizeWarningLimit: 600,
        rollupOptions: { input: { main: "src/client/main.ts" } },
      },
  test: {
    environment: "node",
    include: ["src/**/*.test.ts", "src/**/*.test.tsx"],
  },
}));
