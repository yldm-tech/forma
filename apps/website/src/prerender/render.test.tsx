import { describe, expect, it } from "vite-plus/test";
import { DICTIONARIES } from "../content";
import { LOCALES, ROUTES } from "../site";
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
      const skips = levels.flatMap((level, i) => (i > 0 && level > levels[i - 1] + 1 ? [`h${levels[i - 1]} → h${level}`] : []));
      expect(skips).toEqual([]);
      expect(html).not.toMatch(/<section[^>]* aria-label=/);
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

  it("keeps the anchor the app links to on the security page", async () => {
    const html = await renderPath("/security/", assets);
    expect(html).toContain('id="stay-informed-with-forma-security-updates"');
  });
});

describe("sitemap, robots and redirects", () => {
  it("lists 24 URLs with alternates", () => {
    const sitemap = buildSitemap();
    expect(sitemap.match(/<url>/g)).toHaveLength(24);
    expect(sitemap.match(/hreflang="x-default"/g)).toHaveLength(24);
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
