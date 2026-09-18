/**
 * The translation catalogue's top-level keys, treated as i18next namespaces.
 *
 * `locales/<locale>.json` stays the single source of truth — Lingo.dev, `pnpm i18n` and
 * `scan-translations` all keep reading it unchanged. These lists only decide which slices of it the
 * server hands to the client, and they exist because the slices differ by two orders of magnitude:
 * `workspace` is 136 KB and `templates` 63 KB, against 15 KB for everything a respondent can see.
 *
 * Keys are addressed as `<namespace>.<rest>` (`common.welcome`, `workspace.settings.general.title`),
 * which is the shape every `t()` call in this repo already uses. i18next splits the namespace off
 * the first separator, so no call site changes.
 */

/** Loaded by the root layout, so every route has them before the first paint. */
export const PUBLIC_I18N_NAMESPACES = [
  "common",
  "s",
  "c",
  "auth",
  "setup",
  "organizations",
  "billing_confirmation",
] as const;

/**
 * Added by the `(app)` layout. Behind login, and 199 KB together — a respondent must never pay for
 * it, and an authenticated user must not see raw keys while it arrives, so it is preloaded there
 * rather than fetched after hydration.
 *
 * `templates` is here rather than lazy because keeping it lazy bought nothing: the client's only
 * way to reach one namespace was to import the whole catalogue, so the 263 KB file stayed in the
 * browser bundle to serve a 63 KB slice. Preloading it is what lets the client stop importing
 * `locales/` at all.
 *
 * The trade is deliberate. An admin hard-reload now carries these inline in the RSC payload instead
 * of in a cacheable chunk, costing roughly 25 KB brotli; in exchange a public route carries ~3 KB
 * instead of 61 KB and spends no round trip on it. Respondents are the volume path here.
 */
export const APP_I18N_NAMESPACES = ["workspace", "templates"] as const;

/** Rendered only by `@forma/email` on the server; never reaches a browser. */
export const SERVER_ONLY_I18N_NAMESPACES = ["emails"] as const;

export type I18nNamespace = string;

export const ALL_CLIENT_I18N_NAMESPACES: readonly string[] = [
  ...PUBLIC_I18N_NAMESPACES,
  ...APP_I18N_NAMESPACES,
];
