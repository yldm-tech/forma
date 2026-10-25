// @vitest-environment happy-dom
import { afterAll, beforeAll, beforeEach, describe, expect, test, vi } from "vite-plus/test";

// Where a customer page loaded the runtime from; its locale files sit beside it, under /js/surveys-locales/.
const RUNTIME_URL = "https://forma.example/js/surveys.umd.js";
const LOCALE_PATH = /^\/js\/surveys-locales\/([A-Za-z-]+)\.[0-9a-f]{12}\.json$/;

// packages/surveys/locales, read through Vite as the build reads them.
const catalogues = import.meta.glob<Record<string, unknown>>("../../locales/*.json", {
  eager: true,
  import: "default",
});

/** Serves the locale files as the server serves them from the build. */
const serveLocaleFiles = vi.fn(async (input: URL | string) => {
  const url = new URL(input);
  const match = url.origin === "https://forma.example" ? LOCALE_PATH.exec(url.pathname) : null;
  const catalogue = match ? catalogues[`../../locales/${match[1]}.json`] : undefined;
  if (!catalogue) return new Response("Not found", { status: 404 });
  return new Response(JSON.stringify(catalogue), { headers: { "Content-Type": "application/json" } });
});

/**
 * The i18n configuration, evaluated the way the UMD bundle is: while `document.currentScript` is the runtime's script element. It configures i18next's shared instance, so it is evaluated once and the tests share what it has loaded; each test uses languages the others do not.
 */
const loadRuntime = async () => {
  Object.defineProperty(document, "currentScript", { configurable: true, get: () => ({ src: RUNTIME_URL }) });
  try {
    return await import("./i18n.config");
  } finally {
    Reflect.deleteProperty(document, "currentScript");
  }
};

let runtime: Awaited<ReturnType<typeof loadRuntime>>;

beforeAll(async () => {
  vi.stubGlobal("fetch", serveLocaleFiles);
  runtime = await loadRuntime();
});

beforeEach(() => {
  serveLocaleFiles.mockClear();
});

afterAll(() => {
  vi.unstubAllGlobals();
});

describe("languages fetched on demand", () => {
  test("English is in the bundle and needs no request", async () => {
    const { default: i18n, loadSurveyLanguage } = runtime;

    expect(loadSurveyLanguage("en-US")).toBeUndefined();
    expect(i18n.getFixedT("en-US")("common.required")).not.toBe("common.required");
    expect(serveLocaleFiles).not.toHaveBeenCalled();
  });

  test("every other shipped language is fetched from beside the runtime script and then resolves", async () => {
    const { default: i18n, loadSurveyLanguage } = runtime;
    const english = i18n.getFixedT("en-US")("common.required");

    for (const code of ["id-ID", "km-KH", "ne-NP", "ur-PK", "vi-VN", "zh-Hans-CN", "zh-Hant-TW"]) {
      const loading = loadSurveyLanguage(code);
      expect(loading).toBeInstanceOf(Promise);
      await loading;

      const required = i18n.getFixedT(code)("common.required");
      expect(required).not.toBe("common.required");
      expect(required).not.toBe(english);
      // Loaded once, it renders at once from then on.
      expect(loadSurveyLanguage(code)).toBeUndefined();
    }
    for (const [url] of serveLocaleFiles.mock.calls) {
      expect(String(url)).toMatch(/^https:\/\/forma\.example\/js\/surveys-locales\//);
    }
  });

  test("a legacy code loads the bundle of its language", async () => {
    const { default: i18n, loadSurveyLanguage } = runtime;

    await loadSurveyLanguage("de");

    expect(i18n.language).toBe("de-DE");
    expect(i18n.t("common.required")).toBe(i18n.getFixedT("de-DE")("common.required"));
    expect(i18n.t("common.required")).not.toBe(i18n.getFixedT("en-US")("common.required"));
  });

  test("the current language stays until the next one has arrived, so no key or English flashes up", async () => {
    const { default: i18n } = runtime;
    await i18n.changeLanguage("de-DE");
    const german = i18n.t("common.required");

    const switching = i18n.changeLanguage("ja-JP");
    expect(i18n.language).toBe("de-DE");
    expect(i18n.t("common.required")).toBe(german);

    await switching;
    expect(i18n.language).toBe("ja-JP");
    expect(i18n.t("common.required")).not.toBe(german);
  });

  test("a language whose file cannot be fetched renders in English", async () => {
    serveLocaleFiles.mockResolvedValueOnce(new Response("Not found", { status: 404 }));
    const { default: i18n, loadSurveyLanguage } = runtime;

    await loadSurveyLanguage("fr-FR");

    expect(i18n.getFixedT("fr-FR")("common.required")).toBe(i18n.getFixedT("en-US")("common.required"));
  });
});
