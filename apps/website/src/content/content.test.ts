import { describe, expect, it } from "vite-plus/test";
import { LOCALES, PAGES } from "../site";
import { DICTIONARIES, validateDictionaries } from "./index";
import { siteContentSchema } from "./schema";

describe("website content", () => {
  it("parses in every locale", () => {
    expect(() => validateDictionaries()).not.toThrow();
  });

  it("has a dictionary for every locale and every page in each", () => {
    expect(Object.keys(DICTIONARIES).sort()).toEqual([...LOCALES].sort());
    for (const locale of LOCALES) {
      expect(Object.keys(DICTIONARIES[locale].pages).sort()).toEqual([...PAGES].sort());
    }
  });

  it("fails on a missing key and names the locale and the path", () => {
    const { security: _dropped, ...pages } = DICTIONARIES.ja.pages;
    expect(() => validateDictionaries({ ja: { ...DICTIONARIES.ja, pages } })).toThrow(/ja: pages\.security/);
  });

  it("fails on an empty string and on a key the schema does not know", () => {
    const empty = { ...DICTIONARIES.zh, notFound: { ...DICTIONARIES.zh.notFound, title: " " } };
    expect(siteContentSchema.safeParse(empty).success).toBe(false);
    const extra = { ...DICTIONARIES.en, notFound: { ...DICTIONARIES.en.notFound, subtitle: "x" } };
    expect(siteContentSchema.safeParse(extra).success).toBe(false);
  });

  // The design spec rules these out until the license is decided and the claims can be verified.
  it("does not claim what the project cannot back up", () => {
    const text = JSON.stringify(DICTIONARIES).toLowerCase();
    for (const claim of [
      "open source",
      "open-source",
      "agpl",
      "开源",
      "オープンソース",
      "@forma/js",
      "gemini",
    ]) {
      expect(text).not.toContain(claim);
    }
  });
});
