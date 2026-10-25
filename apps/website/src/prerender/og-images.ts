// Draws the 1200 × 630 share image of every page in every locale into public/_site/og/<locale>/<page>.png (package.json `og-images`). Run by hand when a page title changes, and commit the result: the images change rarely and drawing them needs a browser, which the image build does not have.
//
// Each image is the logo (the symbol and the drawn wordmark, components/Logo.tsx) at the top left, the page's heading in the locale's system font, ink on white, and a row of ink and crimson bars echoing the hero's chart. Flat colours only, so each PNG stays small.
import { chromium } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { Logo } from "../components/Logo";
import { DICTIONARIES } from "../content";
import { HTML_LANG, ROUTES, ogImagePath } from "../site";

const PUBLIC = fileURLToPath(new URL("../public", import.meta.url));

const FONTS: Record<string, string> = {
  en: 'ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif',
  zh: 'system-ui, -apple-system, "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", "Noto Sans SC", sans-serif',
  ja: 'system-ui, -apple-system, "Hiragino Sans", "Hiragino Kaku Gothic ProN", "Yu Gothic UI", "Noto Sans JP", sans-serif',
};

const escapeHtml = (value: string): string =>
  value.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");

const BARS = [
  ["#0f172a", 120],
  ["#0f172a", 200],
  ["#0f172a", 150],
  ["#d3264a", 260],
  ["#0f172a", 190],
] as const;

const template = (locale: string, heading: string, eyebrow: string): string => `<!doctype html>
<html lang="${HTML_LANG[locale as keyof typeof HTML_LANG]}"><head><meta charset="utf-8"><style>
  * { margin: 0; box-sizing: border-box; }
  body { width: 1200px; height: 630px; background: #fff; font-family: ${FONTS[locale]}; color: #0f172a; position: relative; overflow: hidden; }
  .glow { position: absolute; right: -180px; top: -260px; width: 720px; height: 720px; border-radius: 50%; background: #fcf2f4; }
  .brand { position: absolute; left: 80px; top: 72px; display: flex; }
  .eyebrow { position: absolute; left: 80px; top: 196px; font-size: 26px; font-weight: 600; color: #b1203e; }
  h1 { position: absolute; left: 80px; top: 244px; width: 700px; font-size: ${locale === "en" ? 58 : 52}px; line-height: 1.18; font-weight: 700; letter-spacing: ${locale === "en" ? "-0.02em" : "0"}; text-wrap: balance; word-break: ${locale === "ja" ? "auto-phrase" : locale === "zh" ? "keep-all" : "normal"}; overflow-wrap: break-word; }
  .bars { position: absolute; right: 96px; bottom: 0; display: flex; align-items: flex-end; gap: 22px; }
  .bars span { display: block; width: 56px; border-radius: 14px 14px 0 0; }
  .rule { position: absolute; left: 0; right: 0; bottom: 0; height: 10px; background: #d3264a; }
</style></head><body>
  <div class="glow"></div>
  <div class="brand">
    ${renderToStaticMarkup(createElement(Logo, { height: 64 }))}
  </div>
  <p class="eyebrow">${escapeHtml(eyebrow)}</p>
  <h1>${escapeHtml(heading)}</h1>
  <div class="bars">${BARS.map(([color, height]) => `<span style="background:${color};height:${height}px"></span>`).join("")}</div>
  <div class="rule"></div>
</body></html>`;

const main = async (): Promise<void> => {
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage({ viewport: { width: 1200, height: 630 }, deviceScaleFactor: 1 });
    for (const route of ROUTES) {
      const content = DICTIONARIES[route.locale];
      const heading = route.page === "home" ? content.pages.home.hero.title : content.pages[route.page].title;
      const eyebrow = route.page === "home" ? content.pages.home.hero.eyebrow : "forma.yldm.ai";
      await page.setContent(template(route.locale, heading, eyebrow), { waitUntil: "load" });
      const file = join(PUBLIC, ogImagePath(route.locale, route.page));
      mkdirSync(dirname(file), { recursive: true });
      await page.screenshot({ path: file, type: "png" });
      console.log(`  ${ogImagePath(route.locale, route.page)}`);
    }
  } finally {
    await browser.close();
  }
};

await main();
