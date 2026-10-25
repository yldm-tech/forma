// @vitest-environment happy-dom
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, test, vi } from "vite-plus/test";

// Where a customer page loaded the runtime from; its locale files sit beside it, under /js/surveys-locales/.
const RUNTIME_URL = "https://forma.example/js/surveys.umd.js";
const LOCALE_PATH = /^\/js\/surveys-locales\/([A-Za-z-]+)\.[0-9a-f]{12}\.json$/;

// packages/surveys/locales, read through Vite as the build reads them.
const catalogues = import.meta.glob<Record<string, unknown>>("../../locales/*.json", {
  eager: true,
  import: "default",
});

/** Answers a locale file as the server serves it from the build. */
const localeFileResponse = (input: URL | string): Response => {
  const url = new URL(input);
  const match = url.origin === "https://forma.example" ? LOCALE_PATH.exec(url.pathname) : null;
  const catalogue = match ? catalogues[`../../locales/${match[1]}.json`] : undefined;
  if (!catalogue) return new Response("Not found", { status: 404 });
  return new Response(JSON.stringify(catalogue), { headers: { "Content-Type": "application/json" } });
};

const serveLocaleFiles = vi.fn(async (input: URL | string, _init?: RequestInit) => localeFileResponse(input));

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

describe("a locale file that is slow or does not arrive", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  test("a fetch that never answers is abandoned, and the language renders in English", async () => {
    vi.useFakeTimers();
    serveLocaleFiles.mockImplementationOnce(
      (_input, init) =>
        new Promise((_resolve, reject) => {
          init?.signal?.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")));
        })
    );
    const { default: i18n, loadSurveyLanguage, LOCALE_FETCH_TIMEOUT_MS } = runtime;

    let settled = false;
    void loadSurveyLanguage("es-ES")?.then(() => {
      settled = true;
    });
    await vi.advanceTimersByTimeAsync(LOCALE_FETCH_TIMEOUT_MS);

    expect(settled).toBe(true);
    // A request that ran out of time is not retried: the server did not answer, and would not the next time either.
    expect(serveLocaleFiles).toHaveBeenCalledTimes(1);
    expect(i18n.getFixedT("es-ES")("common.required")).toBe(i18n.getFixedT("en-US")("common.required"));
  });

  test("a dropped connection is retried twice within about a second, then the language renders in English", async () => {
    vi.useFakeTimers();
    for (let i = 0; i < 3; i++) serveLocaleFiles.mockRejectedValueOnce(new TypeError("Failed to fetch"));
    const { default: i18n, loadSurveyLanguage } = runtime;

    let settled = false;
    void loadSurveyLanguage("it-IT")?.then(() => {
      settled = true;
    });
    // Backing off 350 ms, then 700 ms.
    await vi.advanceTimersByTimeAsync(1100);

    expect(settled).toBe(true);
    expect(serveLocaleFiles).toHaveBeenCalledTimes(3);
    expect(i18n.getFixedT("it-IT")("common.required")).toBe(i18n.getFixedT("en-US")("common.required"));
  });

  test("switching to a language still on its way and back again stays on the language switched back to", async () => {
    const { default: i18n, loadSurveyLanguage, switchSurveyLanguage } = runtime;
    await loadSurveyLanguage("nl-NL");
    const dutch = i18n.t("common.required");

    let arrive = () => {};
    const arrived = new Promise<void>((resolve) => {
      arrive = resolve;
    });
    serveLocaleFiles.mockImplementationOnce(async (input) => {
      await arrived;
      return localeFileResponse(input);
    });

    switchSurveyLanguage("sv-SE");
    expect(i18n.language).toBe("nl-NL");
    // The respondent changes their mind before Swedish has arrived.
    switchSurveyLanguage("nl-NL");

    arrive();
    // Waits for the read the switch to Swedish started, after which that switch has finished or been dropped.
    await i18n.loadLanguages("sv-SE");
    expect(i18n.hasResourceBundle("sv-SE", "translation")).toBe(true);
    expect(i18n.language).toBe("nl-NL");
    expect(i18n.t("common.required")).toBe(dutch);
  });

  test("switching to the language already on its way starts no second switch", async () => {
    const { default: i18n, loadSurveyLanguage, switchSurveyLanguage } = runtime;
    const changeLanguage = vi.spyOn(i18n, "changeLanguage");

    const loading = loadSurveyLanguage("ro-RO");
    switchSurveyLanguage("ro-RO");
    await loading;
    switchSurveyLanguage("ro-RO");

    expect(changeLanguage).toHaveBeenCalledTimes(1);
    expect(i18n.language).toBe("ro-RO");
    changeLanguage.mockRestore();
  });
});
