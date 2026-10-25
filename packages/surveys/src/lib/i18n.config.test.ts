import { readdirSync } from "node:fs";
import localeFiles from "virtual:survey-locale-files";
import { describe, expect, test, vi } from "vite-plus/test";
import { SURVEY_RUNTIME_LANGUAGE_CODES } from "@forma/i18n-utils/survey-runtime-languages";
import i18n, { loadSurveyLanguage, resolveFallbackBundles } from "./i18n.config";

// Locks down the locale-to-bundle fallback contract (ENG-1067). Bundles are keyed by each language's
// canonical CLDR-default tag (`de-DE`, `pt-BR`, `zh-Hans-CN`); resolveFallbackBundles maps any requested
// tag to the bundle we actually ship, then English. SCRIPT is preserved (so Traditional Chinese never
// borrows the Simplified bundle); region is not. The list it returns is what i18next tries *after* the
// requested tag itself, so a request that already equals its default bundle just yields ["en-US"].
describe("resolveFallbackBundles", () => {
  test("a non-default region resolves to its language's default bundle", () => {
    expect(resolveFallbackBundles("de-AT")).toEqual(["de-DE", "en-US"]);
    expect(resolveFallbackBundles("pt-PT")).toEqual(["pt-BR", "en-US"]);
    expect(resolveFallbackBundles("ar-SA")).toEqual(["ar-EG", "en-US"]);
  });

  test("a bare language resolves to its canonical default bundle", () => {
    expect(resolveFallbackBundles("de")).toEqual(["de-DE", "en-US"]);
  });

  test("case is normalized (BCP-47 is case-insensitive)", () => {
    expect(resolveFallbackBundles("DE-de")).toEqual(["de-DE", "en-US"]);
  });

  test("the canonical default tag itself only falls back to English", () => {
    expect(resolveFallbackBundles("de-DE")).toEqual(["en-US"]);
  });

  test("script is preserved: Traditional Chinese never borrows the Simplified bundle", () => {
    // zh-Hant-TW carries an explicit Hant script -> resolves to the Traditional default, which equals the
    // requested tag, so it falls straight through to English. Crucially NOT zh-Hans-CN.
    expect(resolveFallbackBundles("zh-Hant-TW")).toEqual(["en-US"]);
    expect(resolveFallbackBundles("zh-Hant-TW")).not.toContain("zh-Hans-CN");
    // zh-Hant without a region resolves to the Traditional default tag, which is the shipped bundle.
    expect(resolveFallbackBundles("zh-Hant")).toEqual(["zh-Hant-TW", "en-US"]);
    // Legacy tags carry the script only in the region; recovering it keeps them off the Simplified bundle.
    expect(resolveFallbackBundles("zh-TW")).toEqual(["zh-Hant-TW", "en-US"]);
    expect(resolveFallbackBundles("zh-HK")).toEqual(["zh-Hant-TW", "en-US"]);
    // Simplified legacy tags still resolve to Simplified.
    expect(resolveFallbackBundles("zh-CN")).toEqual(["zh-Hans-CN", "en-US"]);
    expect(resolveFallbackBundles("zh")).toEqual(["zh-Hans-CN", "en-US"]);
  });

  test("recovering the script does not leak a script subtag into non-script languages", () => {
    // de-AT/pt-PT canonicalize to themselves (both are catalog tags), so no script is recovered and the
    // region is still dropped — a Latin-script tag like `de-Latn-DE` would miss every shipped bundle.
    expect(resolveFallbackBundles("de-AT")).toEqual(["de-DE", "en-US"]);
    expect(resolveFallbackBundles("pt-PT")).toEqual(["pt-BR", "en-US"]);
    expect(resolveFallbackBundles("ur-IN")).toEqual(["ur-PK", "en-US"]);
  });

  test("the newly shipped languages resolve to their own bundle", () => {
    expect(resolveFallbackBundles("id")).toEqual(["id-ID", "en-US"]);
    expect(resolveFallbackBundles("ur")).toEqual(["ur-PK", "en-US"]);
    expect(resolveFallbackBundles("vi")).toEqual(["vi-VN", "en-US"]);
    expect(resolveFallbackBundles("km")).toEqual(["km-KH", "en-US"]);
    expect(resolveFallbackBundles("ne")).toEqual(["ne-NP", "en-US"]);
  });

  test("an unknown but syntactically valid tag falls back to English", () => {
    expect(resolveFallbackBundles("xx")).toEqual(["en-US"]);
  });

  test("invalid or empty tags fall back to English", () => {
    expect(resolveFallbackBundles("")).toEqual(["en-US"]);
    expect(resolveFallbackBundles("123")).toEqual(["en-US"]);
    expect(resolveFallbackBundles("!!!")).toEqual(["en-US"]);
    // Underscore is not a valid BCP-47 separator -> Intl.Locale throws -> English (NOT de-DE).
    expect(resolveFallbackBundles("DE_de")).toEqual(["en-US"]);
  });
});

describe("shipped bundles", () => {
  // A language registered as supported without a bundle silently renders English (ENG-2068). English is bundled; every other language is a file the runtime fetches (i18n.config.fetch.test.ts).
  test("every supported language has a translation bundle registered", () => {
    const supportedLngs = i18n.options.supportedLngs || [];
    const supported = supportedLngs.filter((code: string) => code !== "cimode");
    expect(Object.keys(i18n.options.resources ?? {})).toEqual(["en-US"]);
    expect([...supported].sort()).toEqual(["en-US", ...Object.keys(localeFiles)].sort());
  });

  // The file names carry a hash of their content, which is what lets the server cache them as immutable: a changed translation must reach respondents under a new name.
  test("every fetched language is a content-addressed file beside the runtime", () => {
    for (const [code, file] of Object.entries(localeFiles)) {
      expect(file).toMatch(new RegExp(`^surveys-locales/${code}\\.[0-9a-f]{12}\\.json$`));
    }
  });

  // SURVEY_RUNTIME_LANGUAGE_CODES is what the workspace default-survey-language picker offers
  // (ENG-2816). Offering a language whose bundle we do not ship is the bug that setting exists to
  // avoid, so the list has to be provably the shipped set — not a copy of it that can rot.
  test("the runtime language list matches the bundles on disk", () => {
    const shippedBundleCodes = readdirSync(new URL("../../locales/", import.meta.url))
      .filter((file) => file.endsWith(".json"))
      .map((file) => file.replace(/\.json$/, ""));

    expect([...SURVEY_RUNTIME_LANGUAGE_CODES].sort()).toEqual(shippedBundleCodes.sort());
  });

  // The widget runs on customers' sites, so it must not print i18next's vendor notice in their console.
  test("i18next's support notice is off", () => {
    expect(i18n.options.showSupportNotice).toBe(false);
  });
});

// This file runs without a document, so the runtime has no script element to locate its files by, as on a host that put the runtime's code into its page instead of loading the script.
describe("languages fetched without a runtime script", () => {
  test("are fetched from the app the survey is rendered for", async () => {
    const fetchLocale = vi.fn((_url: URL) =>
      Promise.resolve(new Response(JSON.stringify({ common: { required: "Obbligatorio" } })))
    );
    vi.stubGlobal("fetch", fetchLocale);
    try {
      await loadSurveyLanguage("it-IT", "https://forma.example/");

      expect(String(fetchLocale.mock.calls[0]?.[0])).toBe(`https://forma.example/js/${localeFiles["it-IT"]}`);
      expect(i18n.getFixedT("it-IT")("common.required")).toBe("Obbligatorio");
    } finally {
      vi.unstubAllGlobals();
    }
  });
});
