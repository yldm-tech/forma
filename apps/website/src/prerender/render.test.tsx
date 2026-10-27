import { describe, expect, it } from "vite-plus/test";
import { DICTIONARIES } from "../content";
import { APP_LOCALE, LOCALES, ROUTES, localeOfPath } from "../site";
import { ROBOTS_TXT, buildRedirectsConf, buildSitemap, notFoundProbePath, renderPath } from "./render";

const assets = { stylesheets: ["/_site/assets/main.css"], script: "/_site/assets/main.js" };

describe("prerendered pages", () => {
  it.each(ROUTES.map((route) => [route.path, route] as const))(
    "%s carries its content and metadata",
    async (path, route) => {
      const html = await renderPath(path, assets);
      const content = DICTIONARIES[route.locale];
      const meta = content.pages[route.page].meta;
      const lang = { en: "en", zh: "zh-Hans", ja: "ja" }[route.locale];

      expect(html.startsWith("<!doctype html><html")).toBe(true);
      expect(html).toContain(`<html lang="${lang}">`);
      expect(html).toContain(`<link rel="canonical" href="https://forma.yldm.ai${path}"/>`);
      for (const alternate of ["en", "zh-Hans", "ja", "x-default"]) {
        expect(html).toContain(`hrefLang="${alternate}"`);
      }
      expect(html).toContain('<meta property="og:type" content="website"/>');
      expect(html).toContain(`content="${meta.description.replace(/&/g, "&amp;")}"`);
      expect(html).toMatch(/<h1[^>]*>/);
      expect(html).toContain('<script type="module" src="/_site/assets/main.js">');
      // Static only: no hydration payload from the router or React.
      expect(html).not.toContain("__TSR");
      expect(html).not.toMatch(/<script>(?!<\/script>)/);
    }
  );

  // WCAG 1.3.1: each heading is at most one level below the one before it, and a section is a landmark only when a heading of its own names it.
  it.each([...ROUTES.map((route) => route.path), ...LOCALES.map(notFoundProbePath)])(
    "%s keeps its heading order and names no section after the page",
    async (path) => {
      const html = await renderPath(path, assets);
      const levels = [...html.matchAll(/<h([1-6])[\s>]/g)].map((match) => Number(match[1]));
      expect(levels[0]).toBe(1);
      const skips = levels.flatMap((level, i) => {
        const previous = levels[i - 1];
        return previous !== undefined && level > previous + 1 ? [`h${previous} → h${level}`] : [];
      });
      expect(skips).toEqual([]);
      expect(html).not.toMatch(/<section[^>]* aria-label=/);
    }
  );

  // The documentation site is retired, so no page may link to /docs.
  it.each([...ROUTES.map((route) => route.path), ...LOCALES.map(notFoundProbePath)])(
    "%s links to no documentation",
    async (path) => {
      const html = await renderPath(path, assets);
      expect(html).not.toMatch(/href="(https:\/\/forma\.yldm\.ai)?\/docs/);
    }
  );

  it("renders the home hero in each locale", async () => {
    for (const locale of LOCALES) {
      const route = ROUTES.find((r) => r.locale === locale && r.page === "home");
      const html = await renderPath(route?.path ?? "/", assets);
      expect(html).toContain(DICTIONARIES[locale].pages.home.hero.title);
    }
  });

  it("renders the 404 page in the locale of the path, without a canonical", async () => {
    const html = await renderPath(notFoundProbePath("ja"), assets);
    expect(html).toContain('<html lang="ja">');
    expect(html).toContain(DICTIONARIES.ja.notFound.title);
    expect(html).toContain('<meta name="robots" content="noindex"/>');
    expect(html).not.toContain('rel="canonical"');
  });

  // A visitor who reads the site in Japanese should land on a Japanese sign-in page, whatever their browser's language.
  it.each([...ROUTES.map((route) => route.path), ...LOCALES.map(notFoundProbePath)])(
    "%s links to the app in its own language",
    async (path) => {
      const html = await renderPath(path, assets);
      const appLinks = [...html.matchAll(/href="(\/app\/[^"]*)"/g)].map((match) => match[1] ?? "");
      expect(appLinks.length).toBeGreaterThan(0);
      const lang = APP_LOCALE[localeOfPath(path)];
      expect(appLinks.filter((href) => !href.endsWith(`?lang=${lang}`))).toEqual([]);
    }
  );

  it.each(ROUTES.map((route) => route.path))("%s links only to fragments it has", async (path) => {
    const html = await renderPath(path, assets);
    const ids = new Set([...html.matchAll(/\sid="([^"]+)"/g)].map((match) => match[1]));
    const fragments = [...html.matchAll(/href="#([^"]+)"/g)].map((match) => match[1]);
    expect(fragments.filter((fragment) => !ids.has(fragment))).toEqual([]);
  });

  it("links the developers page's API and MCP cards to its reference", async () => {
    for (const locale of LOCALES) {
      const route = ROUTES.find((r) => r.locale === locale && r.page === "developers");
      const html = await renderPath(route?.path ?? "/developers/", assets);
      expect(html).toContain('href="#rest-api"');
      expect(html).toContain('href="#mcp"');
      expect(html).toContain("X-Api-Key");
    }
  });

  // The app's Web SDK page links here (apps/web/src/lib/sdk-snippets.ts sdkReferenceUrl), and so does the developers page.
  it("renders the SDK reference at the path the app links to, linked from the developers page", async () => {
    for (const locale of LOCALES) {
      const sdk = ROUTES.find((r) => r.locale === locale && r.page === "sdk");
      expect(sdk?.path).toBe(locale === "en" ? "/developers/sdk/" : `/${locale}/developers/sdk/`);
      const html = await renderPath(sdk?.path ?? "/developers/sdk/", assets);
      for (const method of [
        "setUserId",
        "setAttributes",
        "setLanguage",
        "track",
        "logout",
        "registerRouteChange",
        "setEmbeddedData",
        "setNonce",
        "formaDebug",
        "forma_setup_successful",
      ]) {
        expect(html).toContain(method);
      }
      expect(html).toContain("/js/forma.umd.js");
      const developers = ROUTES.find((r) => r.locale === locale && r.page === "developers");
      expect(await renderPath(developers?.path ?? "/developers/", assets)).toContain(
        `href="${sdk?.path ?? ""}"`
      );
    }
  });

  it("keeps the anchor the app links to on the security page", async () => {
    const html = await renderPath("/security/", assets);
    expect(html).toContain('id="stay-informed-with-forma-security-updates"');
  });
});

describe("sitemap, robots and redirects", () => {
  it("lists 27 URLs with alternates", () => {
    const sitemap = buildSitemap();
    expect(sitemap.match(/<url>/g)).toHaveLength(27);
    expect(sitemap.match(/hreflang="x-default"/g)).toHaveLength(27);
    expect(sitemap).toContain("<loc>https://forma.yldm.ai/ja/security/</loc>");
    expect(sitemap).not.toContain("<lastmod>");
  });

  it("keeps crawlers out of the app", () => {
    expect(ROBOTS_TXT).toContain("Disallow: /app/");
    expect(ROBOTS_TXT).toContain("Sitemap: https://forma.yldm.ai/sitemap.xml");
  });

  it("writes one exact-match location per redirect, with and without a trailing slash", () => {
    const conf = buildRedirectsConf([
      { from: "/github", to: "https://github.com/yldm-tech/forma", status: 302 },
    ]);
    expect(conf).toContain('location = /github { return 302 "https://github.com/yldm-tech/forma"; }');
    expect(conf).toContain('location = /github/ { return 302 "https://github.com/yldm-tech/forma"; }');
  });
});
