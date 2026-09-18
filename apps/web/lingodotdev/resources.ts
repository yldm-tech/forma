import "server-only";
import { logger } from "@forma/logger";
import { DEFAULT_LOCALE } from "@/lib/constants";

/** One locale's resources, keyed by namespace, in the shape i18next's `resources` option takes. */
export type I18nResourceBundle = Record<string, Record<string, unknown>>;

/**
 * Reads the requested namespaces out of a locale's catalogue so a server component can hand them to
 * the client provider.
 *
 * The whole catalogue is a Node-side import, so slicing it costs nothing here; what matters is that
 * only the slice crosses to the browser. This is what replaces the client-side
 * `resourcesToBackend` fetch that used to run after hydration — and the reason the provider can now
 * initialise synchronously and render on the server.
 *
 * Missing namespaces are dropped rather than thrown on: a namespace that is absent degrades to
 * i18next's fallback for those keys, where throwing would take down the page.
 */
export const loadI18nResources = async (
  locale: string,
  namespaces: readonly string[]
): Promise<I18nResourceBundle> => {
  const catalogue = await importCatalogue(locale);
  if (!catalogue) return {};

  const bundle: I18nResourceBundle = {};
  for (const namespace of namespaces) {
    const slice = catalogue[namespace];
    if (slice && typeof slice === "object") {
      bundle[namespace] = slice as Record<string, unknown>;
    }
  }
  return bundle;
};

const importCatalogue = async (locale: string): Promise<Record<string, unknown> | null> => {
  try {
    // Template literal on purpose: the bundler turns it into a context over `locales/*.json`, so
    // every shipped locale resolves without listing them here.
    const loaded = (await import(`../locales/${locale}.json`)) as { default?: Record<string, unknown> };
    return loaded.default ?? (loaded as Record<string, unknown>);
  } catch (error) {
    if (locale === DEFAULT_LOCALE) {
      logger.error({ error, locale }, "Failed to load the default translation catalogue");
      return null;
    }
    logger.warn({ error, locale }, "No translation catalogue for locale; falling back to the default");
    return importCatalogue(DEFAULT_LOCALE);
  }
};
