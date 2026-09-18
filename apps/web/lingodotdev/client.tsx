"use client";

import i18n from "i18next";
import ICU from "i18next-icu";
import { ReactNode } from "react";
import { I18nextProvider, initReactI18next } from "react-i18next";
import { logger } from "@forma/logger";
import { buildI18nInitOptions } from "@/lingodotdev/init";

/**
 * Initialised during render rather than in an effect, from resources the server already read.
 *
 * The provider used to `init()` inside `useEffect` and `return null` until it resolved. An effect
 * does not run on the server, so this returned null there for every route — including the public
 * link survey — and the response carried no application markup at all. First paint waited on
 * hydration plus a 61 KB catalogue chunk, and a failed chunk fetch left a permanently blank page.
 *
 * i18next initialises synchronously when it is handed `resources` instead of a backend, so doing it
 * in the module body (idempotent, before the first render) removes both the effect and the gate. The
 * race the old comment guarded against is gone with them: there is no window where the tree is
 * mounted and the catalogue is not.
 *
 * There is no backend plugin any more either. The client's only way to load one namespace was to
 * import the whole 263 KB catalogue and pick a key out of it, so lazy loading kept the catalogue in
 * the browser bundle to save nothing. Every namespace now arrives from the server — the root layout
 * for the public set, `(app)` for the admin set — and `locales/` is server-side only.
 */

interface I18nProviderProps {
  children: ReactNode;
  language: string;
  defaultLanguage: string;
  /** Namespaces for `language`, read server-side by `loadI18nResources`. */
  resources: Record<string, Record<string, unknown>>;
}

let isInit = false;

const ensureInitialised = ({ language, defaultLanguage, resources }: Omit<I18nProviderProps, "children">) => {
  if (isInit) {
    addResources(language, resources);
    if (i18n.language !== language) {
      // Synchronous when the target language's resources are already loaded, which they are: the
      // callback form only matters for a backend fetch.
      void i18n.changeLanguage(language);
    }
    return;
  }

  try {
    i18n.use(ICU).use(initReactI18next).init(buildI18nInitOptions({ language, defaultLanguage, resources }));
    isInit = true;
  } catch (error) {
    // A catalogue that will not load must not cost the page. i18next falls back to rendering keys.
    logger.error(error);
    isInit = true;
  }
};

/** Namespaces a nested layout loaded (see `AppI18nResources`) are merged into the live instance. */
const addResources = (language: string, resources: Record<string, Record<string, unknown>>) => {
  for (const [namespace, bundle] of Object.entries(resources)) {
    if (i18n.hasResourceBundle(language, namespace)) continue;
    i18n.addResourceBundle(language, namespace, bundle, true, false);
  }
};

export const I18nProvider = ({ children, language, defaultLanguage, resources }: I18nProviderProps) => {
  ensureInitialised({ language, defaultLanguage, resources });

  return (
    <I18nextProvider data-testid="i18next-provider" i18n={i18n}>
      {children}
    </I18nextProvider>
  );
};

/**
 * Merges a route group's extra namespaces into the instance the root layout already initialised.
 *
 * Renders nothing. It exists so `(app)` can preload its 136 KB `workspace` namespace server-side
 * without the public routes paying for it, and without an authenticated user seeing raw keys while
 * it arrives.
 */
export const I18nResources = ({
  language,
  resources,
}: Readonly<{ language: string; resources: Record<string, Record<string, unknown>> }>) => {
  addResources(language, resources);
  return null;
};
