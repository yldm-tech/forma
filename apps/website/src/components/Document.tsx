import type { ReactNode } from "react";
import { DICTIONARIES } from "../content";
import {
  HTML_LANG,
  LOCALES,
  type Locale,
  OG_LOCALE,
  type PageId,
  absoluteUrl,
  localePrefix,
  ogImagePath,
  pagePath,
} from "../site";
import { SiteFooter } from "./Footer";
import { SiteHeader } from "./Header";
import { SiteContext } from "./site-context";

/** The built client files a page links to (from Vite's manifest), as root-relative URLs. */
export interface DocumentAssets {
  stylesheets: string[];
  script: string;
}

const LocaleHintTemplate = ({ locale, page }: { locale: Locale; page: PageId | undefined }) => {
  const hint = DICTIONARIES[locale].common.localeHint;
  return (
    <template data-locale-hint={locale}>
      <div
        lang={HTML_LANG[locale]}
        className="border-crimson-100 bg-crimson-50 text-small text-crimson-900 border-b"
        data-locale-hint-bar>
        <div className="container-site flex min-h-11 items-center justify-between gap-4 py-1.5">
          <a
            href={page ? pagePath(locale, page) : localePrefix(locale)}
            hrefLang={HTML_LANG[locale]}
            className="link text-crimson-700 hover:text-crimson-900">
            {hint.text}
          </a>
          <button
            type="button"
            data-locale-hint-dismiss
            className="rounded-chip hover:bg-crimson-100 px-2 py-1 font-medium">
            {hint.dismiss}
          </button>
        </div>
      </div>
    </template>
  );
};

export const Document = ({
  locale,
  page,
  assets,
  children,
}: {
  locale: Locale;
  page: PageId | undefined;
  assets: DocumentAssets;
  children: ReactNode;
}) => {
  const content = DICTIONARIES[locale];
  const meta = page ? content.pages[page].meta : content.notFound.meta;
  const url = page ? absoluteUrl(pagePath(locale, page)) : undefined;
  const ogImage = absoluteUrl(ogImagePath(locale, page ?? "home"));

  const structuredData = page
    ? [
        {
          "@context": "https://schema.org",
          "@type": "SoftwareApplication",
          name: "Forma",
          applicationCategory: "BusinessApplication",
          operatingSystem: "Web",
          url,
          inLanguage: HTML_LANG[locale],
        },
        ...(page === "home"
          ? [
              {
                "@context": "https://schema.org",
                "@type": "WebSite",
                name: "Forma",
                url: absoluteUrl(localePrefix(locale)),
                inLanguage: HTML_LANG[locale],
              },
            ]
          : []),
      ]
    : [];

  return (
    <html lang={HTML_LANG[locale]}>
      <head>
        <meta charSet="utf-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1" />
        <title>{meta.title}</title>
        <meta name="description" content={meta.description} />
        <meta name="theme-color" content="#d3264a" />
        {page ? null : <meta name="robots" content="noindex" />}
        {url ? <link rel="canonical" href={url} /> : null}
        {page
          ? LOCALES.map((alt) => (
              <link
                key={alt}
                rel="alternate"
                hrefLang={HTML_LANG[alt]}
                href={absoluteUrl(pagePath(alt, page))}
              />
            ))
          : null}
        {page ? <link rel="alternate" hrefLang="x-default" href={absoluteUrl(pagePath("en", page))} /> : null}
        {/* /favicon.ico is the server's (apps/web/public/favicon.ico, the same icon): the ingress sends it there. */}
        <link rel="icon" href="/favicon.ico" sizes="any" />
        <link rel="icon" type="image/png" sizes="32x32" href="/_site/icons/favicon-32x32.png" />
        <link rel="apple-touch-icon" href="/_site/icons/apple-touch-icon.png" />
        <meta property="og:type" content="website" />
        <meta property="og:site_name" content="Forma" />
        <meta property="og:title" content={meta.title} />
        <meta property="og:description" content={meta.description} />
        {url ? <meta property="og:url" content={url} /> : null}
        <meta property="og:locale" content={OG_LOCALE[locale]} />
        {LOCALES.filter((alt) => alt !== locale).map((alt) => (
          <meta key={alt} property="og:locale:alternate" content={OG_LOCALE[alt]} />
        ))}
        <meta property="og:image" content={ogImage} />
        <meta property="og:image:width" content="1200" />
        <meta property="og:image:height" content="630" />
        <meta name="twitter:card" content="summary_large_image" />
        {assets.stylesheets.map((href) => (
          <link key={href} rel="stylesheet" href={href} />
        ))}
        <script type="module" src={assets.script} />
        {structuredData.length > 0 ? (
          <script
            type="application/ld+json"
            // Escaping `<` keeps a string in the data from closing the script element.
            dangerouslySetInnerHTML={{ __html: JSON.stringify(structuredData).replace(/</g, "\\u003c") }}
          />
        ) : null}
      </head>
      <body className="bg-surface flex min-h-dvh flex-col antialiased">
        <SiteContext.Provider value={{ locale, page, content }}>
          {/* The base :focus-visible ring is a box-shadow, so the raised shadow would replace it; the ring utilities compose with --tw-shadow and keep the crimson ring. */}
          <a
            href="#main"
            className="rounded-button focus:shadow-raised focus:ring-crimson-600 sr-only z-50 bg-white px-4 py-2 font-semibold focus:not-sr-only focus:fixed focus:top-3 focus:left-3 focus:px-4 focus:py-2 focus:ring-2 focus:ring-offset-2">
            {content.common.skipToContent}
          </a>
          <SiteHeader />
          <div data-locale-hint-slot />
          <main id="main" tabIndex={-1} className="flex-1 outline-none">
            {children}
          </main>
          <SiteFooter />
          {LOCALES.filter((hint) => hint !== locale && hint !== "en").map((hint) => (
            <LocaleHintTemplate key={hint} locale={hint} page={page} />
          ))}
        </SiteContext.Provider>
      </body>
    </html>
  );
};
