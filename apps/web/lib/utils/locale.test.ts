import * as nextHeaders from "next/headers";
import { describe, expect, test, vi } from "vitest";
import { DEFAULT_LOCALE } from "@/lib/constants";
import { findMatchingLocale } from "./locale";

// Mock the Next.js headers function
vi.mock("next/headers", () => ({
  headers: vi.fn(),
}));

type RequestHeaders = Awaited<ReturnType<typeof nextHeaders.headers>>;

const withAcceptLanguage = (value: string | null) => {
  const requestHeaders = new Headers(value === null ? undefined : { "accept-language": value });
  vi.mocked(nextHeaders.headers).mockResolvedValue(requestHeaders as RequestHeaders);
};

/**
 * These assertions run against the real `AVAILABLE_LOCALES` — `["en-US", "ja-JP", "zh-Hans-CN", "zh-Hant-TW"]`.
 *
 * They used not to: `vitestSetup.ts` overrode the constant with the 15 locales of the upstream project, so the suite exercised a language list this app does not ship (one test asserted `sv-SE` was selectable) and could not see either bug below, both of which only exist because two of the four shipped locales share a base language.
 */
describe("findMatchingLocale", () => {
  test("returns DEFAULT_LOCALE when Accept-Language header is missing", async () => {
    withAcceptLanguage(null);

    expect(await findMatchingLocale()).toBe(DEFAULT_LOCALE);
    expect(nextHeaders.headers).toHaveBeenCalled();
  });

  test("returns an exact match", async () => {
    withAcceptLanguage("ja-JP,fr-FR,de-DE");

    expect(await findMatchingLocale()).toBe("ja-JP");
  });

  test("falls back to the same language in another region", async () => {
    withAcceptLanguage("en-GB,fr-FR");

    expect(await findMatchingLocale()).toBe("en-US");
  });

  test("returns DEFAULT_LOCALE when no match is found", async () => {
    withAcceptLanguage("xx-XX,yy-YY");

    expect(await findMatchingLocale()).toBe(DEFAULT_LOCALE);
  });

  test.each([
    ["zh-TW,zh;q=0.9,en;q=0.8", "zh-Hant-TW"],
    ["zh-HK,en;q=0.7", "zh-Hant-TW"],
    ["zh-Hant,en;q=0.7", "zh-Hant-TW"],
    ["zh-CN,en;q=0.7", "zh-Hans-CN"],
    ["zh-Hans-CN", "zh-Hans-CN"],
    // No script and no region: CLDR resolves bare `zh` to Simplified, and so does the list order here.
    ["zh", "zh-Hans-CN"],
  ])("routes %s to %s", async (header, expected) => {
    withAcceptLanguage(header);

    expect(await findMatchingLocale()).toBe(expected);
  });

  test("honours entries after the first, which carry a leading space and a q weight", async () => {
    // Chrome and Safari both write the separator as ", ". The old matcher compared the raw entry, so " ja-JP;q=0.9" matched nothing and this fell through to English.
    withAcceptLanguage("fr-FR, ja-JP;q=0.9");

    expect(await findMatchingLocale()).toBe("ja-JP");
  });

  test("prefers the higher-weighted language over header order", async () => {
    withAcceptLanguage("en-US;q=0.5, ja-JP;q=0.9");

    expect(await findMatchingLocale()).toBe("ja-JP");
  });

  test("ignores a language the client explicitly refused with q=0", async () => {
    withAcceptLanguage("ja-JP;q=0, en-US;q=0.5");

    expect(await findMatchingLocale()).toBe("en-US");
  });

  test("matches case-insensitively", async () => {
    withAcceptLanguage("JA-jp");

    expect(await findMatchingLocale()).toBe("ja-JP");
  });

  test("returns DEFAULT_LOCALE for a wildcard-only header", async () => {
    withAcceptLanguage("*");

    expect(await findMatchingLocale()).toBe(DEFAULT_LOCALE);
  });
});
