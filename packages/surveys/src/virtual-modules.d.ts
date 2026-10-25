// Modules the Vite config provides (vite.config.mts).

declare module "virtual:survey-locale-files" {
  /** The translation bundles the runtime fetches on demand, every shipped language but English: the language's tag to its file, a path relative to the runtime script (`surveys-locales/de-DE.<hash>.json`). */
  const localeFiles: Readonly<Record<string, string>>;
  export default localeFiles;
}
