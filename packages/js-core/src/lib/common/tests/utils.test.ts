// utils.test.ts
import { type Mock, afterEach, beforeEach, describe, expect, test, vi } from "vite-plus/test";
import { SAMPLING_ID_STORAGE_KEY } from "@/lib/common/constants";
import { Logger } from "@/lib/common/logger";
import { mockSurveyId } from "@/lib/common/tests/__mocks__/config.mock";
import {
  checkUrlMatch,
  diffInDays,
  evaluateNoCodeConfigClick,
  filterSurveys,
  getDefaultLanguageCode,
  getIsDebug,
  getLanguageCode,
  getAnonymousSamplingId,
  getSamplingBucket,
  getSecureRandom,
  getStyling,
  getSurveyExclusionReason,
  getSurveyKey,
  handleUrlFilters,
  isNowExpired,
  normalizeAppUrl,
  shouldDisplayBasedOnPercentage,
  toSurveyKeys,
  wrapThrows,
  wrapThrowsAsync,
} from "@/lib/common/utils";
import type {
  TUserState,
  TWorkspaceState,
  TWorkspaceStateActionClass,
  TWorkspaceStateSettings,
  TWorkspaceStateSurvey,
} from "@/types/config";
import { type TActionClassNoCodeConfig, type TActionClassPageUrlRule } from "@/types/survey";

const mockSurveyId1 = "e3kxlpnzmdp84op9qzxl9olj";
const mockSurveyId2 = "qo9rwjmms42hoy3k85fp8vgu";
const mockSegmentId1 = "p6yrnz3s2tvoe5r0l28unq7k";
const mockSegmentId2 = "wz43zrxeddhb1uo9cicustar";

describe("utils.ts", () => {
  // ---------------------------------------------------------------------------------
  // diffInDays
  // ---------------------------------------------------------------------------------
  describe("diffInDays()", () => {
    test("calculates correct day difference", () => {
      const date1 = new Date("2023-01-01");
      const date2 = new Date("2023-01-05");
      expect(diffInDays(date1, date2)).toBe(4); // four days apart
    });

    test("handles negative differences (abs)", () => {
      const date1 = new Date("2023-01-10");
      const date2 = new Date("2023-01-05");
      expect(diffInDays(date1, date2)).toBe(5);
    });

    test("0 if same day", () => {
      const date = new Date("2023-01-01");
      expect(diffInDays(date, date)).toBe(0);
    });
  });

  // ---------------------------------------------------------------------------------
  // wrapThrows
  // ---------------------------------------------------------------------------------
  describe("wrapThrows()", () => {
    test("returns ok on success", () => {
      const fn = vi.fn(() => "success");
      const wrapped = wrapThrows(fn);
      const result = wrapped();
      expect(result.ok).toBe(true);
      if (result.ok) {
        expect(result.data).toBe("success");
      }
    });

    test("returns err on error", () => {
      const fn = vi.fn(() => {
        throw new Error("Something broke");
      });
      const wrapped = wrapThrows(fn);
      const result = wrapped();
      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.error.message).toBe("Something broke");
      }
    });

    test("passes arguments to wrapped function", () => {
      const fn = vi.fn((a: number, b: number) => a + b);
      const wrapped = wrapThrows(fn);
      const result = wrapped(2, 3);
      expect(result.ok).toBe(true);
      if (result.ok) {
        expect(result.data).toBe(5);
      }
      expect(fn).toHaveBeenCalledWith(2, 3);
    });

    test("handles async function", () => {
      const fn = vi.fn(async () => {
        await new Promise((r) => {
          setTimeout(r, 10);
        });
        return "async success";
      });
      const wrapped = wrapThrows(fn);
      const result = wrapped();
      expect(result.ok).toBe(true);
      if (result.ok) {
        expect(result.data).toBeInstanceOf(Promise);
      }
    });
  });

  // ---------------------------------------------------------------------------------
  // wrapThrowsAsync
  // ---------------------------------------------------------------------------------
  describe("wrapThrowsAsync()", () => {
    test("returns ok on success", async () => {
      const fn = vi.fn(async (x: number) => {
        await new Promise((r) => {
          setTimeout(r, 10);
        });
        return x * 2;
      });

      const wrapped = wrapThrowsAsync(fn);

      const result = await wrapped(5);
      expect(result.ok).toBe(true);
      if (result.ok) {
        expect(result.data).toBe(10);
      }
    });

    test("returns err on error", async () => {
      const fn = vi.fn(async () => {
        await new Promise((r) => {
          setTimeout(r, 10);
        });
        throw new Error("Something broke");
      });
      const wrapped = wrapThrowsAsync(fn);

      const result = await wrapped();
      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.error.message).toBe("Something broke");
      }
    });
  });

  // ---------------------------------------------------------------------------------
  // filterSurveys
  // ---------------------------------------------------------------------------------
  describe("filterSurveys()", () => {
    // We'll create a minimal workspace state
    let workspace: TWorkspaceState;
    let user: TUserState;
    const baseSurvey: Partial<TWorkspaceStateSurvey> = {
      id: mockSurveyId,
      displayOption: "displayOnce",
      displayLimit: 1,
      recontactDays: null,
      languages: [],
    };

    beforeEach(() => {
      workspace = {
        expiresAt: new Date(),
        data: {
          settings: {
            recontactDays: 7, // fallback if survey doesn't have it
            clickOutsideClose: false,
            overlay: "none",
            placement: "bottomRight",
            inAppSurveyBranding: true,
            styling: { allowStyleOverwrite: false },
          },
          surveys: [],
          actionClasses: [],
        },
      };
      user = {
        expiresAt: null,
        data: {
          userId: null,
          contactId: null,
          segments: [],
          displays: [],
          responses: [],
          lastDisplayAt: null,
        },
      };
    });

    test("returns no surveys if user has no segments and userId is set", () => {
      user.data.userId = "user_abc";
      // workspace has a single survey
      workspace.data.surveys = [
        { ...baseSurvey, id: mockSurveyId1, segment: { id: mockSegmentId1 } } as TWorkspaceStateSurvey,
      ];

      const result = filterSurveys(workspace, user);
      expect(result).toEqual([]); // no segments => none pass
    });

    test("returns surveys if user has no userId but displayOnce and no displays yet", () => {
      // userId is null => it won't segment filter
      workspace.data.surveys = [
        { ...baseSurvey, id: mockSurveyId1, displayOption: "displayOnce" } as TWorkspaceStateSurvey,
      ];

      const result = filterSurveys(workspace, user);
      expect(result).toHaveLength(1);
      expect(result[0].id).toBe(mockSurveyId1);
    });

    test("skips surveys that already displayed if displayOnce is used", () => {
      workspace.data.surveys = [
        { ...baseSurvey, id: mockSurveyId1, displayOption: "displayOnce" } as TWorkspaceStateSurvey,
      ];
      user.data.displays = [getSurveyKey(null, mockSurveyId1)];

      const result = filterSurveys(workspace, user);
      expect(result).toEqual([]);
    });

    test("skips surveys if user responded to them and displayOption=displayMultiple", () => {
      workspace.data.surveys = [
        { ...baseSurvey, id: mockSurveyId1, displayOption: "displayMultiple" } as TWorkspaceStateSurvey,
      ];
      user.data.responses = [getSurveyKey(null, mockSurveyId1)];

      const result = filterSurveys(workspace, user);
      expect(result).toEqual([]);
    });

    test("handles displaySome logic with displayLimit", () => {
      workspace.data.surveys = [
        {
          ...baseSurvey,
          id: mockSurveyId1,
          displayOption: "displaySome",
          displayLimit: 2,
        } as TWorkspaceStateSurvey,
      ];
      // user has 1 display of s1
      user.data.displays = [getSurveyKey(null, mockSurveyId1)];

      // No responses => so it's still allowed
      const result = filterSurveys(workspace, user);
      expect(result).toHaveLength(1);
    });

    test("filters out surveys if recontactDays not met", () => {
      // Suppose survey uses workspace fallback (7 days)
      workspace.data.surveys = [
        { ...baseSurvey, id: mockSurveyId1, displayOption: "displayOnce" } as TWorkspaceStateSurvey,
      ];
      // user last displayAt is only 3 days ago
      user.data.lastDisplayAt = new Date(Date.now() - 3 * 24 * 60 * 60 * 1000);

      const result = filterSurveys(workspace, user);
      expect(result).toHaveLength(0);
    });

    test("passes surveys if enough days have passed since lastDisplayAt", () => {
      // user last displayAt is 8 days ago
      user.data.lastDisplayAt = new Date(Date.now() - 8 * 24 * 60 * 60 * 1000);

      workspace.data.surveys = [
        {
          ...baseSurvey,
          id: mockSurveyId1,
          displayOption: "respondMultiple",
          recontactDays: null,
        } as TWorkspaceStateSurvey,
      ];
      const result = filterSurveys(workspace, user);
      expect(result).toHaveLength(1);
    });

    test("anonymous user: excludes segment-targeted surveys (new shape: hasFilters=true)", () => {
      workspace.data.surveys = [
        {
          ...baseSurvey,
          id: mockSurveyId1,
          segment: { id: mockSegmentId1, hasFilters: true },
          displayOption: "respondMultiple",
        } as TWorkspaceStateSurvey,
        {
          ...baseSurvey,
          id: mockSurveyId2,
          segment: { id: mockSegmentId2, hasFilters: false },
          displayOption: "respondMultiple",
        } as TWorkspaceStateSurvey,
      ];

      const result = filterSurveys(workspace, user);
      expect(result).toHaveLength(1);
      expect(result[0].id).toBe(mockSurveyId2);
    });

    test("anonymous user: excludes segment-targeted surveys when cached payload uses legacy shape (filters array)", () => {
      // Simulates a localStorage payload written by an older SDK version that
      // still has `segment.filters` and no `hasFilters`. The defensive check
      // must fall back to the legacy shape so anonymous users don't receive
      // segment-targeted surveys.
      workspace.data.surveys = [
        {
          ...baseSurvey,
          id: mockSurveyId1,
          segment: { id: mockSegmentId1, filters: [{ type: "attribute", value: "plan" }] },
          displayOption: "respondMultiple",
        } as unknown as TWorkspaceStateSurvey,
        {
          ...baseSurvey,
          id: mockSurveyId2,
          segment: { id: mockSegmentId2, filters: [] },
          displayOption: "respondMultiple",
        } as unknown as TWorkspaceStateSurvey,
      ];

      const result = filterSurveys(workspace, user);
      expect(result).toHaveLength(1);
      expect(result[0].id).toBe(mockSurveyId2);
    });

    test("filters by segment if userId is set and user has segments", () => {
      user.data.userId = "user_abc";
      user.data.segments = [mockSegmentId1];
      workspace.data.surveys = [
        {
          ...baseSurvey,
          id: mockSurveyId1,
          segment: { id: mockSegmentId1 },
          displayOption: "respondMultiple",
        } as TWorkspaceStateSurvey,
        {
          ...baseSurvey,
          id: mockSurveyId2,
          segment: { id: mockSegmentId2 },
          displayOption: "respondMultiple",
        } as TWorkspaceStateSurvey,
      ];

      const result = filterSurveys(workspace, user);
      // only the one that matches user's segment
      expect(result).toHaveLength(1);
      expect(result[0].id).toBe(mockSurveyId1);
    });
  });

  // ---------------------------------------------------------------------------------
  // getStyling
  // ---------------------------------------------------------------------------------
  describe("getStyling()", () => {
    test("returns workspace styling if allowStyleOverwrite=false", () => {
      const settings = {
        styling: { allowStyleOverwrite: false, brandColor: { light: "#fff" } },
      } as TWorkspaceStateSettings;
      const survey = {
        styling: {
          overwriteThemeStyling: true,
          brandColor: { light: "#000" },
        },
      } as TWorkspaceStateSurvey;

      const result = getStyling(settings, survey);
      // should get workspace styling
      expect(result).toEqual(settings.styling);
    });

    test("returns workspace styling if allowStyleOverwrite=true but survey overwriteThemeStyling=false", () => {
      const settings = {
        styling: { allowStyleOverwrite: true, brandColor: { light: "#fff" } },
      } as TWorkspaceStateSettings;
      const survey = {
        styling: {
          overwriteThemeStyling: false,
          brandColor: { light: "#000" },
        },
      } as TWorkspaceStateSurvey;

      const result = getStyling(settings, survey);
      // should get workspace styling still
      expect(result).toEqual(settings.styling);
    });

    test("returns survey styling if allowStyleOverwrite=true and survey overwriteThemeStyling=true", () => {
      const settings = {
        styling: { allowStyleOverwrite: true, brandColor: { light: "#fff" } },
      } as TWorkspaceStateSettings;
      const survey = {
        styling: {
          overwriteThemeStyling: true,
          brandColor: { light: "#000" },
        },
      } as TWorkspaceStateSurvey;

      const result = getStyling(settings, survey);
      expect(result).toEqual(survey.styling);
    });
  });

  // ---------------------------------------------------------------------------------
  // getDefaultLanguageCode
  // ---------------------------------------------------------------------------------
  describe("getDefaultLanguageCode()", () => {
    test("returns code of the language if it is flagged default", () => {
      const survey = {
        languages: [
          {
            language: { code: "en" },
            default: false,
            enabled: true,
          },
          {
            language: { code: "fr" },
            default: true,
            enabled: true,
          },
        ],
      } as unknown as TWorkspaceStateSurvey;
      expect(getDefaultLanguageCode(survey)).toBe("fr");
    });

    test("returns undefined if no default language found", () => {
      const survey = {
        languages: [
          { language: { code: "en" }, default: false, enabled: true },
          { language: { code: "fr" }, default: false, enabled: true },
        ],
      } as unknown as TWorkspaceStateSurvey;
      expect(getDefaultLanguageCode(survey)).toBeUndefined();
    });
  });

  // ---------------------------------------------------------------------------------
  // getLanguageCode
  // ---------------------------------------------------------------------------------
  describe("getLanguageCode()", () => {
    test("returns 'default' if no language param is passed", () => {
      const survey = {
        languages: [{ language: { code: "en" }, default: true, enabled: true }],
      } as unknown as TWorkspaceStateSurvey;
      const code = getLanguageCode(survey, undefined);
      expect(code).toBe("default");
    });

    test("returns 'default' if the chosen language is the default one", () => {
      const survey = {
        languages: [
          { language: { code: "en" }, default: true, enabled: true },
          { language: { code: "fr" }, default: false, enabled: true },
        ],
      } as unknown as TWorkspaceStateSurvey;
      const code = getLanguageCode(survey, "en");
      expect(code).toBe("default");
    });

    test("returns undefined if language not found or disabled", () => {
      const survey = {
        languages: [
          { language: { code: "en" }, default: true, enabled: true },
          { language: { code: "fr" }, default: false, enabled: false },
        ],
      } as unknown as TWorkspaceStateSurvey;
      const code = getLanguageCode(survey, "fr");
      expect(code).toBeUndefined();
    });

    test("returns the language code if found and enabled", () => {
      const survey = {
        languages: [
          { language: { code: "en", alias: "English" }, default: true, enabled: true },
          { language: { code: "fr", alias: "fr-FR" }, default: false, enabled: true },
        ],
      } as unknown as TWorkspaceStateSurvey;
      expect(getLanguageCode(survey, "fr")).toBe("fr");
      expect(getLanguageCode(survey, "fr-FR")).toBe("fr");
    });
  });

  // ---------------------------------------------------------------------------------
  // shouldDisplayBasedOnPercentage
  // ---------------------------------------------------------------------------------
  describe("shouldDisplayBasedOnPercentage()", () => {
    test("gives the same user the same answer on every trigger", () => {
      const first = shouldDisplayBasedOnPercentage(10, "contact_1", mockSurveyId1);
      for (let i = 0; i < 100; i++) {
        expect(shouldDisplayBasedOnPercentage(10, "contact_1", mockSurveyId1)).toBe(first);
      }
    });

    test("samples roughly the configured share of users", () => {
      let shown = 0;
      for (let i = 0; i < 10000; i++) {
        if (shouldDisplayBasedOnPercentage(30, `contact_${i.toString()}`, mockSurveyId1)) shown++;
      }
      expect(shown / 10000).toBeGreaterThan(0.28);
      expect(shown / 10000).toBeLessThan(0.32);
    });

    test("buckets the same user independently per survey", () => {
      const users = Array.from({ length: 1000 }, (_, i) => `contact_${i.toString()}`);
      const inFirst = users.filter((u) => shouldDisplayBasedOnPercentage(50, u, mockSurveyId1));
      const inBoth = inFirst.filter((u) => shouldDisplayBasedOnPercentage(50, u, mockSurveyId2));
      // Independent 50% samples overlap on about half of the first sample, not all of it.
      expect(inBoth.length / inFirst.length).toBeGreaterThan(0.4);
      expect(inBoth.length / inFirst.length).toBeLessThan(0.6);
    });

    test("shows to everyone at 100% and nobody below the smallest bucket", () => {
      for (let i = 0; i < 200; i++) {
        expect(shouldDisplayBasedOnPercentage(100, `contact_${i.toString()}`, mockSurveyId1)).toBe(true);
        expect(shouldDisplayBasedOnPercentage(0.001, `contact_${i.toString()}`, mockSurveyId1)).toBe(false);
      }
    });

    test("matches the bucket boundary exactly", () => {
      const bucket = getSamplingBucket("contact_1", mockSurveyId1);
      expect(bucket).toBeGreaterThanOrEqual(0);
      expect(bucket).toBeLessThan(10000);
      expect(shouldDisplayBasedOnPercentage((bucket + 1) / 100, "contact_1", mockSurveyId1)).toBe(true);
      expect(shouldDisplayBasedOnPercentage(bucket / 100, "contact_1", mockSurveyId1)).toBe(false);
    });
  });

  // ---------------------------------------------------------------------------------
  // getAnonymousSamplingId
  // ---------------------------------------------------------------------------------
  describe("getAnonymousSamplingId()", () => {
    afterEach(() => {
      vi.mocked(localStorage.getItem).mockReset();
      vi.mocked(localStorage.setItem).mockReset();
    });

    test("creates the id once and reuses the stored one afterwards", () => {
      const store = new Map<string, string>();
      vi.mocked(localStorage.getItem).mockImplementation((key: string) => store.get(key) ?? null);
      vi.mocked(localStorage.setItem).mockImplementation((key: string, value: string) => {
        store.set(key, value);
      });

      const first = getAnonymousSamplingId();
      expect(first).toMatch(/^[0-9a-f]{32}$/);
      expect(store.get(SAMPLING_ID_STORAGE_KEY)).toBe(first);
      expect(getAnonymousSamplingId()).toBe(first);
    });

    test("keeps one id for the page when localStorage throws", () => {
      vi.mocked(localStorage.getItem).mockImplementation(() => {
        throw new Error("SecurityError");
      });

      const first = getAnonymousSamplingId();
      expect(getAnonymousSamplingId()).toBe(first);
    });
  });

  // ---------------------------------------------------------------------------------
  // normalizeAppUrl
  // ---------------------------------------------------------------------------------
  describe("normalizeAppUrl()", () => {
    test("strips trailing slashes and surrounding whitespace", () => {
      expect(normalizeAppUrl("http://localhost:3800/")).toBe("http://localhost:3800");
      expect(normalizeAppUrl(" https://forma.yldm.ai// ")).toBe("https://forma.yldm.ai");
      expect(normalizeAppUrl("https://example.com/forma")).toBe("https://example.com/forma");
    });
  });

  // ---------------------------------------------------------------------------------
  // isNowExpired
  // ---------------------------------------------------------------------------------
  describe("isNowExpired()", () => {
    test("returns true if expiration date is in the past", () => {
      const expirationDate = new Date(Date.now() - 1000);
      expect(isNowExpired(expirationDate)).toBe(true);
    });

    test("returns false if expiration date is in the future", () => {
      const expirationDate = new Date(Date.now() + 1000);
      expect(isNowExpired(expirationDate)).toBe(false);
    });
  });

  // ---------------------------------------------------------------------------------
  // checkUrlMatch
  // ---------------------------------------------------------------------------------
  describe("checkUrlMatch()", () => {
    test("returns true if url matches", () => {
      const url = "https://example.com/path";
      const pageUrlValue = "https://example.com/path";
      const pageUrlRule = "exactMatch" as unknown as TActionClassPageUrlRule;

      expect(checkUrlMatch(url, pageUrlValue, pageUrlRule)).toBe(true);
    });

    test("returns false if url does not match", () => {
      const url = "https://example.com/path";
      const pageUrlValue = "https://example.com/other";
      const pageUrlRule = "exactMatch" as unknown as TActionClassPageUrlRule;

      expect(checkUrlMatch(url, pageUrlValue, pageUrlRule)).toBe(false);
    });

    test("returns true if url matches with startsWith rule", () => {
      const url = "https://example.com/path";
      const pageUrlValue = "https://example.com";
      const pageUrlRule = "startsWith" as unknown as TActionClassPageUrlRule;

      expect(checkUrlMatch(url, pageUrlValue, pageUrlRule)).toBe(true);
    });

    test("returns false if url does not match with startsWith rule", () => {
      const url = "https://example.com/path";
      const pageUrlValue = "https://example.com/other";
      const pageUrlRule = "startsWith" as unknown as TActionClassPageUrlRule;

      expect(checkUrlMatch(url, pageUrlValue, pageUrlRule)).toBe(false);
    });

    test("returns true if url matches with endsWith rule", () => {
      const url = "https://example.com/path";
      const pageUrlValue = "path";
      const pageUrlRule = "endsWith" as unknown as TActionClassPageUrlRule;

      expect(checkUrlMatch(url, pageUrlValue, pageUrlRule)).toBe(true);
    });

    test("returns false if url does not match with endsWith rule", () => {
      const url = "https://example.com/path";
      const pageUrlValue = "other";
      const pageUrlRule = "endsWith" as unknown as TActionClassPageUrlRule;

      expect(checkUrlMatch(url, pageUrlValue, pageUrlRule)).toBe(false);
    });

    test("returns true if url matches with contains rule", () => {
      const url = "https://example.com/path";
      const pageUrlValue = "path";
      const pageUrlRule = "contains" as unknown as TActionClassPageUrlRule;

      expect(checkUrlMatch(url, pageUrlValue, pageUrlRule)).toBe(true);
    });

    test("returns false if url does not match with contains rule", () => {
      const url = "https://example.com/path";
      const pageUrlValue = "other";
      const pageUrlRule = "contains" as unknown as TActionClassPageUrlRule;

      expect(checkUrlMatch(url, pageUrlValue, pageUrlRule)).toBe(false);
    });

    test("returns true if url matches with notMatch rule", () => {
      const url = "https://example.com/path";
      const pageUrlValue = "https://example.com/other";
      const pageUrlRule = "notMatch" as unknown as TActionClassPageUrlRule;

      expect(checkUrlMatch(url, pageUrlValue, pageUrlRule)).toBe(true);
    });

    test("returns false if url does not match with notMatch rule", () => {
      const url = "https://example.com/path";
      const pageUrlValue = "https://example.com/path";
      const pageUrlRule = "notMatch" as unknown as TActionClassPageUrlRule;

      expect(checkUrlMatch(url, pageUrlValue, pageUrlRule)).toBe(false);
    });

    test("returns true if url matches with notContains rule", () => {
      const url = "https://example.com/path";
      const pageUrlValue = "https://example.com/other";
      const pageUrlRule = "notContains" as unknown as TActionClassPageUrlRule;

      expect(checkUrlMatch(url, pageUrlValue, pageUrlRule)).toBe(true);
    });

    test("returns false if url does not match with notContains rule", () => {
      const url = "https://example.com/path";
      const pageUrlValue = "path";
      const pageUrlRule = "notContains" as unknown as TActionClassPageUrlRule;

      expect(checkUrlMatch(url, pageUrlValue, pageUrlRule)).toBe(false);
    });
  });

  // ---------------------------------------------------------------------------------
  // handleUrlFilters
  // ---------------------------------------------------------------------------------
  describe("handleUrlFilters()", () => {
    test("returns true if url matches with urlFilters", () => {
      const urlFilters = [
        {
          value: "https://example.com/path",
          rule: "exactMatch" as unknown as TActionClassPageUrlRule,
        },
      ];

      // mock window.location.href
      vi.stubGlobal("window", {
        location: {
          href: "https://example.com/path",
        },
      });

      const result = handleUrlFilters(urlFilters);
      expect(result).toBe(true);
    });

    test("returns true if urlFilters is empty", () => {
      const urlFilters: TActionClassNoCodeConfig["urlFilters"] = [];

      const result = handleUrlFilters(urlFilters);
      expect(result).toBe(true);
    });

    test("returns false if no urlFilters match", () => {
      const urlFilters = [
        {
          value: "https://example.com/other",
          rule: "exactMatch" as unknown as TActionClassPageUrlRule,
        },
      ];

      // mock window.location.href
      vi.stubGlobal("window", {
        location: {
          href: "https://example.com/path",
        },
      });

      const result = handleUrlFilters(urlFilters);
      expect(result).toBe(false);
    });

    test("returns true if any urlFilter matches", () => {
      const urlFilters = [
        {
          value: "https://example.com/other",
          rule: "exactMatch" as unknown as TActionClassPageUrlRule,
        },
        {
          value: "path",
          rule: "contains" as unknown as TActionClassPageUrlRule,
        },
      ];

      // mock window.location.href
      vi.stubGlobal("window", {
        location: {
          href: "https://example.com/path",
        },
      });

      const result = handleUrlFilters(urlFilters);
      expect(result).toBe(true);
    });

    test("returns true with OR connector when any urlFilter matches", () => {
      const urlFilters = [
        {
          value: "https://example.com/other",
          rule: "exactMatch" as unknown as TActionClassPageUrlRule,
        },
        {
          value: "path",
          rule: "contains" as unknown as TActionClassPageUrlRule,
        },
      ];

      // mock window.location.href
      vi.stubGlobal("window", {
        location: {
          href: "https://example.com/path",
        },
      });

      const result = handleUrlFilters(urlFilters, "or");
      expect(result).toBe(true);
    });

    test("returns false with OR connector when no urlFilter matches", () => {
      const urlFilters = [
        {
          value: "https://example.com/other",
          rule: "exactMatch" as unknown as TActionClassPageUrlRule,
        },
        {
          value: "different",
          rule: "contains" as unknown as TActionClassPageUrlRule,
        },
      ];

      // mock window.location.href
      vi.stubGlobal("window", {
        location: {
          href: "https://example.com/path",
        },
      });

      const result = handleUrlFilters(urlFilters, "or");
      expect(result).toBe(false);
    });

    test("returns true with AND connector when all urlFilters match", () => {
      const urlFilters = [
        {
          value: "example.com",
          rule: "contains" as unknown as TActionClassPageUrlRule,
        },
        {
          value: "path",
          rule: "contains" as unknown as TActionClassPageUrlRule,
        },
      ];

      // mock window.location.href
      vi.stubGlobal("window", {
        location: {
          href: "https://example.com/path",
        },
      });

      const result = handleUrlFilters(urlFilters, "and");
      expect(result).toBe(true);
    });

    test("returns false with AND connector when not all urlFilters match", () => {
      const urlFilters = [
        {
          value: "example.com",
          rule: "contains" as unknown as TActionClassPageUrlRule,
        },
        {
          value: "different",
          rule: "contains" as unknown as TActionClassPageUrlRule,
        },
      ];

      // mock window.location.href
      vi.stubGlobal("window", {
        location: {
          href: "https://example.com/path",
        },
      });

      const result = handleUrlFilters(urlFilters, "and");
      expect(result).toBe(false);
    });

    test("returns true with AND connector when single filter matches", () => {
      const urlFilters = [
        {
          value: "example.com",
          rule: "contains" as unknown as TActionClassPageUrlRule,
        },
      ];

      // mock window.location.href
      vi.stubGlobal("window", {
        location: {
          href: "https://example.com/path",
        },
      });

      const result = handleUrlFilters(urlFilters, "and");
      expect(result).toBe(true);
    });

    test("returns false with AND connector when single filter does not match", () => {
      const urlFilters = [
        {
          value: "different.com",
          rule: "contains" as unknown as TActionClassPageUrlRule,
        },
      ];

      // mock window.location.href
      vi.stubGlobal("window", {
        location: {
          href: "https://example.com/path",
        },
      });

      const result = handleUrlFilters(urlFilters, "and");
      expect(result).toBe(false);
    });

    test("defaults to OR connector when connector is not specified", () => {
      const urlFilters = [
        {
          value: "https://example.com/other",
          rule: "exactMatch" as unknown as TActionClassPageUrlRule,
        },
        {
          value: "path",
          rule: "contains" as unknown as TActionClassPageUrlRule,
        },
      ];

      // mock window.location.href
      vi.stubGlobal("window", {
        location: {
          href: "https://example.com/path",
        },
      });

      const resultWithoutConnector = handleUrlFilters(urlFilters);
      const resultWithOr = handleUrlFilters(urlFilters, "or");

      expect(resultWithoutConnector).toBe(resultWithOr);
      expect(resultWithoutConnector).toBe(true);
    });
  });

  // ---------------------------------------------------------------------------------
  // evaluateNoCodeConfigClick
  // ---------------------------------------------------------------------------------
  describe("evaluateNoCodeConfigClick()", () => {
    test("returns false if type is not click", () => {
      const targetElement = document.createElement("div");

      const action: TWorkspaceStateActionClass = {
        id: "clabc123abc",
        name: "Test Action",
        type: "noCode",
        key: null,
        noCodeConfig: {
          type: "pageView",
          urlFilters: [],
        },
      };

      const result = evaluateNoCodeConfigClick(targetElement, action);
      expect(result).toBe(false);
    });

    // Elements here are plain stand-ins (the suite runs without a DOM): `closest` answers for the nearest button / link / role=button ancestor the matcher asks for.
    const makeElement = (
      textContent: string,
      innerHTML: string,
      interactiveAncestor: HTMLElement | null = null
    ): HTMLElement =>
      ({
        textContent,
        innerHTML,
        matches: vi.fn(() => false),
        closest: vi.fn(() => interactiveAncestor),
      }) as unknown as HTMLElement;

    const clickAction = (innerHtml: string, cssSelector?: string): TWorkspaceStateActionClass => ({
      id: "clabc123abc",
      name: "Buy click",
      type: "noCode",
      key: null,
      noCodeConfig: {
        type: "click",
        urlFilters: [],
        elementSelector: { innerHtml, ...(cssSelector && { cssSelector }) },
      },
    });

    test("matches the visible text of a button that also holds an icon", () => {
      // <button><svg/> Buy now</button>
      const button = makeElement(" Buy now", '<svg viewBox="0 0 16 16"></svg> Buy now');
      (button.closest as Mock).mockReturnValue(button);

      expect(evaluateNoCodeConfigClick(button, clickAction("Buy now"))).toBe(true);
    });

    test("matches when the click lands on the icon inside the button", () => {
      const button = makeElement(" Buy now", '<svg viewBox="0 0 16 16"></svg> Buy now');
      const svg = makeElement("", "", button);

      expect(evaluateNoCodeConfigClick(svg, clickAction("Buy now"))).toBe(true);
    });

    test("ignores surrounding whitespace and line breaks in server-rendered markup", () => {
      const link = makeElement("\n      Buy\n      now\n    ", "\n      Buy\n      now\n    ");
      (link.closest as Mock).mockReturnValue(link);

      expect(evaluateNoCodeConfigClick(link, clickAction("  Buy now "))).toBe(true);
    });

    test("does not match a different text", () => {
      const button = makeElement("Buy later", "Buy later");
      (button.closest as Mock).mockReturnValue(button);

      expect(evaluateNoCodeConfigClick(button, clickAction("Buy now"))).toBe(false);
    });

    test("does not match a container that merely contains the text", () => {
      // A click on a card's padding: neither the card nor any button ancestor has exactly that text.
      const card = makeElement("Pro plan Buy now", "<h3>Pro plan</h3><button>Buy now</button>");

      expect(evaluateNoCodeConfigClick(card, clickAction("Buy now"))).toBe(false);
    });

    test("still matches an action saved with markup by exact innerHTML", () => {
      const button = makeElement("Buy now", "<span>Buy</span> now");

      expect(evaluateNoCodeConfigClick(button, clickAction("<span>Buy</span> now"))).toBe(true);
    });

    test("matches the text of the element the CSS selector resolved to", () => {
      const button = makeElement("Buy now", '<svg viewBox="0 0 16 16"></svg>Buy now');
      const icon = makeElement("", "<path />", null);
      (icon.closest as Mock).mockImplementation((selector: string) => (selector === ".buy" ? button : null));

      expect(evaluateNoCodeConfigClick(icon, clickAction("Buy now", ".buy"))).toBe(true);
    });

    test("returns false if innerHtml does not match", () => {
      const targetElement = document.createElement("div");
      targetElement.innerHTML = "Test";
      targetElement.textContent = "Test";
      targetElement.closest = vi.fn(() => null);

      const action: TWorkspaceStateActionClass = {
        id: "clabc123abc",
        name: "Test Action",
        type: "noCode",
        key: null,
        noCodeConfig: {
          type: "click",
          urlFilters: [],
          elementSelector: {
            innerHtml: "Testing",
          },
        },
      };

      const result = evaluateNoCodeConfigClick(targetElement, action);
      expect(result).toBe(false);
    });

    test("returns true if cssSelector matches", () => {
      const targetElement = document.createElement("div");
      const cssSelector = ".test";
      targetElement.className = "test";

      targetElement.matches = vi.fn(() => true) as unknown as Element["matches"];

      const action: TWorkspaceStateActionClass = {
        id: "clabc123abc",
        name: "Test Action",
        type: "noCode",
        key: null,
        noCodeConfig: {
          type: "click",
          urlFilters: [],
          elementSelector: {
            cssSelector,
          },
        },
      };

      const result = evaluateNoCodeConfigClick(targetElement, action);
      expect(result).toBe(true);
    });

    test("returns false if cssSelector does not match", () => {
      const targetElement = document.createElement("div");
      const cssSelector = ".test";
      targetElement.className = "other";

      targetElement.matches = vi.fn(() => false) as unknown as Element["matches"];
      targetElement.closest = vi.fn(() => null); // no ancestor matches either

      const action: TWorkspaceStateActionClass = {
        id: "clabc123abc",
        name: "Test Action",
        type: "noCode",
        key: null,
        noCodeConfig: {
          type: "click",
          urlFilters: [],
          elementSelector: {
            cssSelector,
          },
        },
      };

      const result = evaluateNoCodeConfigClick(targetElement, action);
      expect(result).toBe(false);
    });

    test("returns false if neither innerHtml nor cssSelector is provided", () => {
      const targetElement = document.createElement("div");

      const action: TWorkspaceStateActionClass = {
        id: "clabc123abc",
        name: "Test Action",
        type: "noCode",
        key: null,
        noCodeConfig: {
          type: "click",
          urlFilters: [],
          elementSelector: {},
        },
      };

      const result = evaluateNoCodeConfigClick(targetElement, action);
      expect(result).toBe(false);
    });

    test("returns false if urlFilters do not match", () => {
      const targetElement = document.createElement("div");
      targetElement.innerHTML = "Test";

      // mock window.location.href
      vi.stubGlobal("window", {
        location: {
          href: "https://example.com/path",
        },
      });

      const action: TWorkspaceStateActionClass = {
        id: "clabc123abc",
        name: "Test Action",
        type: "noCode",
        key: null,
        noCodeConfig: {
          type: "click",
          urlFilters: [
            {
              value: "https://example.com/other",
              rule: "exactMatch",
            },
          ],
          elementSelector: {
            innerHtml: "Test",
          },
        },
      };

      const result = evaluateNoCodeConfigClick(targetElement, action);
      expect(result).toBe(false);
    });

    test("returns true if both innerHtml and urlFilters match", () => {
      const targetElement = document.createElement("div");
      targetElement.innerHTML = "Test";

      // mock window.location.href
      vi.stubGlobal("window", {
        location: {
          href: "https://example.com/path",
        },
      });

      const action: TWorkspaceStateActionClass = {
        id: "clabc123abc",
        name: "Test Action",
        type: "noCode",
        key: null,
        noCodeConfig: {
          type: "click",
          urlFilters: [
            {
              value: "path",
              rule: "contains",
            },
          ],
          elementSelector: {
            innerHtml: "Test",
          },
        },
      };

      const result = evaluateNoCodeConfigClick(targetElement, action);
      expect(result).toBe(true);
    });

    // --- Regression tests for nested child click target (issue #7314) ---
    // In this test environment document.createElement() returns a plain mock object,
    // so we set .matches and .closest as vi.fn() — the same pattern used by existing tests.
    // This exercises the exact code path of the fix: matches() fails → closest() succeeds.

    test("returns true when clicking a child element inside a button matched by cssSelector", () => {
      const button = document.createElement("button");
      const icon = document.createElement("span");

      // Simulate: icon does NOT directly match ".my-btn", but its closest ancestor does
      (icon as unknown as { matches: ReturnType<typeof vi.fn> }).matches = vi.fn(() => false);
      (icon as unknown as { closest: ReturnType<typeof vi.fn> }).closest = vi.fn(() => button);

      const action: TWorkspaceStateActionClass = {
        id: "clabc123abc",
        name: "Test Action",
        type: "noCode",
        key: null,
        noCodeConfig: {
          type: "click",
          urlFilters: [],
          elementSelector: { cssSelector: ".my-btn" },
        },
      };

      // Before fix: matches() → false → returns false (bug)
      // After fix:  matches() → false → closest() → button → returns true (correct)
      const result = evaluateNoCodeConfigClick(icon, action);
      expect(result).toBe(true);
    });

    test("returns false when clicking a child element with no matching ancestor", () => {
      const other = document.createElement("div");

      // Simulate: element doesn't match, and no ancestor matches either
      (other as unknown as { matches: ReturnType<typeof vi.fn> }).matches = vi.fn(() => false);
      (other as unknown as { closest: ReturnType<typeof vi.fn> }).closest = vi.fn(() => null);

      const action: TWorkspaceStateActionClass = {
        id: "clabc123abc",
        name: "Test Action",
        type: "noCode",
        key: null,
        noCodeConfig: {
          type: "click",
          urlFilters: [],
          elementSelector: { cssSelector: ".my-btn" },
        },
      };

      const result = evaluateNoCodeConfigClick(other, action);
      expect(result).toBe(false);
    });

    test("uses direct target (not closest) when target directly matches cssSelector", () => {
      const button = document.createElement("button");

      // Simulate: click on the button itself — matches() succeeds, closest() should NOT be called
      (button as unknown as { matches: ReturnType<typeof vi.fn> }).matches = vi.fn(() => true);
      const closestSpy = vi.fn();
      (button as unknown as { closest: ReturnType<typeof vi.fn> }).closest = closestSpy;

      const action: TWorkspaceStateActionClass = {
        id: "clabc123abc",
        name: "Test Action",
        type: "noCode",
        key: null,
        noCodeConfig: {
          type: "click",
          urlFilters: [],
          elementSelector: { cssSelector: ".my-btn" },
        },
      };

      const result = evaluateNoCodeConfigClick(button, action);
      expect(result).toBe(true);
      expect(closestSpy).not.toHaveBeenCalled(); // closest() is only a fallback
    });

    test("returns false instead of throwing when the browser cannot parse the cssSelector", () => {
      const targetElement = document.createElement("div");

      // An unparseable selector (`:contains("Buy")`, or one using syntax this browser lacks) makes both
      // DOM calls throw SyntaxError; the click handler loops over every action, so a throw here would
      // kill click tracking for every action after this one.
      targetElement.matches = vi.fn(() => {
        throw new Error("SyntaxError");
      }) as unknown as Element["matches"];
      targetElement.closest = vi.fn(() => {
        throw new Error("SyntaxError");
      });

      const action: TWorkspaceStateActionClass = {
        id: "clabc123abc",
        name: "Test Action",
        type: "noCode",
        key: null,
        noCodeConfig: {
          type: "click",
          urlFilters: [],
          elementSelector: { cssSelector: ':contains("Buy")' },
        },
      };

      expect(evaluateNoCodeConfigClick(targetElement, action)).toBe(false);
    });

    test("handles multiple cssSelectors correctly", () => {
      const targetElement = document.createElement("div");
      targetElement.className = "test other";

      targetElement.matches = vi.fn((selector: string) => {
        return selector === ".test" || selector === ".other" || selector === ".test .other";
      }) as unknown as Element["matches"];
      targetElement.closest = vi.fn(() => null); // not needed but consistent with mock environment

      const action: TWorkspaceStateActionClass = {
        id: "clabc123abc",
        name: "Test Action",
        type: "noCode",
        key: null,
        noCodeConfig: {
          type: "click",
          urlFilters: [],
          elementSelector: {
            cssSelector: ".test .other",
          },
        },
      };

      const result = evaluateNoCodeConfigClick(targetElement, action);
      expect(result).toBe(true);
    });
  });

  // ---------------------------------------------------------------------------------
  // getIsDebug
  // ---------------------------------------------------------------------------------
  describe("getIsDebug()", () => {
    beforeEach(() => {
      // Reset window.location.search before each test
      Object.defineProperty(window, "location", {
        value: { search: "" },
        writable: true,
      });
    });

    test("returns true if debug parameter is set", () => {
      Object.defineProperty(window, "location", {
        value: { search: "?formaDebug=true" },
        writable: true,
      });
      expect(getIsDebug()).toBe(true);
    });

    test("returns false if debug parameter is not set", () => {
      Object.defineProperty(window, "location", {
        value: { search: "?otherParam=value" },
        writable: true,
      });
      expect(getIsDebug()).toBe(false);
    });

    test("returns false if search string is empty", () => {
      Object.defineProperty(window, "location", {
        value: { search: "" },
        writable: true,
      });
      expect(getIsDebug()).toBe(false);
    });

    test("returns false if search string is just '?'", () => {
      Object.defineProperty(window, "location", {
        value: { search: "?" },
        writable: true,
      });
      expect(getIsDebug()).toBe(false);
    });
  });

  // ---------------------------------------------------------------------------------
  // getSecureRandom
  // ---------------------------------------------------------------------------------
  describe("getSecureRandom()", () => {
    test("returns a number between 0 and 1", () => {
      const result = getSecureRandom();
      expect(result).toBeGreaterThanOrEqual(0);
      expect(result).toBeLessThan(1);
    });

    test("returns different values on subsequent calls", () => {
      const result1 = getSecureRandom();
      const result2 = getSecureRandom();
      expect(result1).not.toBe(result2);
    });

    test("uses crypto.getRandomValues", () => {
      const mockGetRandomValues = vi.spyOn(crypto, "getRandomValues");
      getSecureRandom();
      expect(mockGetRandomValues).toHaveBeenCalled();
      mockGetRandomValues.mockRestore();
    });
  });
});

describe("filterSurveys() — interaction targeting × recontact/display-cap matrix", () => {
  // Composition seam (ENG-1275): the client decides show/no-show from server-computed segment
  // membership (`user.segments`) combined with local displayOption / recontactDays / workspace
  // cooldown / lastDisplayAt. Interaction-segment membership is computed server-side, so at this
  // layer it's an input ("is the survey's segment in user.segments?"). This matrix locks the
  // interplay — a survey shows only when membership AND display-cap AND recontact all pass.
  const SURVEY = "e3kxlpnzmdp84op9qzxl9olj";
  const SEGMENT = "p6yrnz3s2tvoe5r0l28unq7k";
  const DAY = 1000 * 60 * 60 * 24;
  const daysAgo = (n: number): Date => new Date(Date.now() - n * DAY);

  const buildWorkspace = (survey: TWorkspaceStateSurvey, cooldownDays: number): TWorkspaceState => ({
    expiresAt: new Date(),
    data: {
      settings: {
        recontactDays: cooldownDays,
        clickOutsideClose: false,
        overlay: "none",
        placement: "bottomRight",
        inAppSurveyBranding: true,
        styling: { allowStyleOverwrite: false },
      },
      surveys: [survey],
      actionClasses: [],
    },
  });

  const buildUser = (data: Partial<TUserState["data"]>): TUserState => ({
    expiresAt: null,
    data: {
      userId: "user_abc",
      contactId: null,
      segments: [],
      displays: [],
      responses: [],
      lastDisplayAt: null,
      ...data,
    },
  });

  const buildSurvey = (survey: Partial<TWorkspaceStateSurvey>): TWorkspaceStateSurvey =>
    ({
      id: SURVEY,
      displayOption: "respondMultiple",
      displayLimit: null,
      recontactDays: null,
      languages: [],
      segment: { id: SEGMENT, hasFilters: true },
      ...survey,
    }) as TWorkspaceStateSurvey;

  const cases: {
    name: string;
    survey: Partial<TWorkspaceStateSurvey>;
    cooldownDays?: number;
    user: Partial<TUserState["data"]>;
    shown: boolean;
  }[] = [
    {
      name: "in segment + respondMultiple → shown",
      survey: { displayOption: "respondMultiple" },
      user: { segments: [SEGMENT] },
      shown: true,
    },
    {
      name: "identified but not in segment → hidden (membership gates it out)",
      survey: { displayOption: "respondMultiple" },
      user: { segments: [] },
      shown: false,
    },
    {
      name: "in segment but displayOnce + already displayed → hidden (display cap beats membership)",
      survey: { displayOption: "displayOnce", displayLimit: 1 },
      user: { segments: [SEGMENT], displays: [getSurveyKey("user_abc", SURVEY)] },
      shown: false,
    },
    {
      name: "in segment + displayOnce + never displayed → shown",
      survey: { displayOption: "displayOnce", displayLimit: 1 },
      user: { segments: [SEGMENT] },
      shown: true,
    },
    {
      name: "in segment but survey recontactDays not elapsed → hidden",
      survey: { displayOption: "respondMultiple", recontactDays: 7 },
      user: { segments: [SEGMENT], lastDisplayAt: daysAgo(2) },
      shown: false,
    },
    {
      name: "in segment + survey recontactDays elapsed → shown",
      survey: { displayOption: "respondMultiple", recontactDays: 7 },
      user: { segments: [SEGMENT], lastDisplayAt: daysAgo(10) },
      shown: true,
    },
    {
      name: "in segment but workspace cooldown not elapsed (survey recontactDays null) → hidden",
      survey: { displayOption: "respondMultiple", recontactDays: null },
      cooldownDays: 7,
      user: { segments: [SEGMENT], lastDisplayAt: daysAgo(3) },
      shown: false,
    },
    {
      name: "in segment + workspace cooldown elapsed → shown",
      survey: { displayOption: "respondMultiple", recontactDays: null },
      cooldownDays: 7,
      user: { segments: [SEGMENT], lastDisplayAt: daysAgo(8) },
      shown: true,
    },
    {
      name: "in segment + displayMultiple + already responded → hidden",
      survey: { displayOption: "displayMultiple" },
      user: { segments: [SEGMENT], responses: [getSurveyKey("user_abc", SURVEY)] },
      shown: false,
    },
    {
      name: "in segment + displayMultiple + no response → shown",
      survey: { displayOption: "displayMultiple" },
      user: { segments: [SEGMENT] },
      shown: true,
    },
    {
      name: "in segment + displaySome under limit → shown",
      survey: { displayOption: "displaySome", displayLimit: 2 },
      user: { segments: [SEGMENT], displays: [getSurveyKey("user_abc", SURVEY)] },
      shown: true,
    },
    {
      name: "in segment + displaySome at limit → hidden",
      survey: { displayOption: "displaySome", displayLimit: 2 },
      user: {
        segments: [SEGMENT],
        displays: [getSurveyKey("user_abc", SURVEY), getSurveyKey("user_abc", SURVEY)],
      },
      shown: false,
    },
    {
      name: "anonymous (no userId) + interaction segment (hasFilters) → hidden (needs identity)",
      survey: { displayOption: "respondMultiple", segment: { id: SEGMENT, hasFilters: true } },
      user: { userId: null, segments: [] },
      shown: false,
    },
    {
      name: "anonymous (no userId) + segment without filters → shown",
      survey: { displayOption: "respondMultiple", segment: { id: SEGMENT, hasFilters: false } },
      user: { userId: null, segments: [] },
      shown: true,
    },
  ];

  test.each(cases)("$name", ({ survey, cooldownDays = 7, user, shown }) => {
    const workspace = buildWorkspace(buildSurvey(survey), cooldownDays);
    const userState = buildUser(user);
    const result = filterSurveys(workspace, userState);
    expect(result.some((s) => s.id === SURVEY)).toBe(shown);
  });
});

describe("getSurveyExclusionReason() — what debug mode says about a survey left out", () => {
  const DAY = 1000 * 60 * 60 * 24;
  const settings = {
    recontactDays: 7,
    clickOutsideClose: false,
    overlay: "none",
    placement: "bottomRight",
    inAppSurveyBranding: true,
    styling: { allowStyleOverwrite: false },
  } as TWorkspaceStateSettings;
  const survey = (overrides: Partial<TWorkspaceStateSurvey> = {}): TWorkspaceStateSurvey =>
    ({
      id: "checkout",
      displayOption: "respondMultiple",
      displayLimit: null,
      recontactDays: null,
      languages: [],
      ...overrides,
    }) as TWorkspaceStateSurvey;
  const user = (data: Partial<TUserState["data"]> = {}): TUserState => ({
    expiresAt: null,
    data: {
      userId: null,
      contactId: null,
      segments: [],
      displays: [],
      responses: [],
      lastDisplayAt: null,
      ...data,
    },
  });
  const shownOnce = [getSurveyKey(null, "checkout")];

  test.each([
    [
      "shows only once and was shown",
      survey({ displayOption: "displayOnce" }),
      user({ displays: shownOnce }),
      "shown once already",
    ],
    [
      "shows until answered and was answered",
      survey({ displayOption: "displayMultiple" }),
      user({ responses: [getSurveyKey(null, "checkout")] }),
      "already responded",
    ],
    [
      "reached its display limit",
      survey({ displayOption: "displaySome", displayLimit: 1 }),
      user({ displays: shownOnce }),
      "shown 1/1 times",
    ],
    [
      "another survey reset the workspace cooldown",
      survey(),
      user({ lastDisplayAt: new Date(Date.now() - 2 * DAY) }),
      "workspace cooldown 7d, last survey shown 2d ago",
    ],
    [
      "its own cooldown has not passed",
      survey({ recontactDays: 3 }),
      user({ lastDisplayAt: new Date() }),
      "survey cooldown 3d, last survey shown 0d ago",
    ],
    [
      "targets a segment and the visitor is anonymous",
      survey({ segment: { id: "pro", hasFilters: true } }),
      user(),
      "segment needs setUserId()",
    ],
    [
      "the identified user is not in its segment",
      survey({ segment: { id: "pro", hasFilters: true } }),
      user({ userId: "u1", segments: ["free"] }),
      "user not in its segment",
    ],
  ])("%s", (_, s, u, reason) => {
    expect(getSurveyExclusionReason(s, settings, u)).toBe(reason);
  });

  test("an eligible survey has no reason, an ignore-cooldown one included", () => {
    expect(getSurveyExclusionReason(survey(), settings, user())).toBeNull();
    expect(
      getSurveyExclusionReason(survey({ recontactDays: 0 }), settings, user({ lastDisplayAt: new Date() }))
    ).toBeNull();
    expect(
      getSurveyExclusionReason(
        survey({ segment: { id: "pro", hasFilters: true } }),
        settings,
        user({ userId: "u1", segments: ["pro"] })
      )
    ).toBeNull();
  });

  test("filterSurveys logs every survey it leaves out, with the reason, in debug mode", () => {
    const log = vi.spyOn(console, "log").mockImplementation(() => undefined);
    const logger = Logger.getInstance();
    logger.configure({ logLevel: "debug" });
    try {
      const workspace = {
        expiresAt: new Date(),
        data: { settings, actionClasses: [], surveys: [survey({ id: "pulse", recontactDays: 0 }), survey()] },
      } as TWorkspaceState;

      const result = filterSurveys(workspace, user({ lastDisplayAt: new Date() }));

      expect(result.map((s) => s.id)).toEqual(["pulse"]);
      expect(log).toHaveBeenCalledTimes(1);
      expect(log.mock.calls[0][0]).toContain(
        "Survey checkout left out: workspace cooldown 7d, last survey shown 0d ago"
      );
    } finally {
      logger.configure({ logLevel: "error" });
      log.mockRestore();
    }
  });
});

describe("survey keys — how the user state names surveys", () => {
  // The server computes the same keys (apps/server/internal/sdkidentity/identity_test.go asserts these values), so the state it sends lines up with the surveys the SDK knows.
  test.each([
    ["user-123", "0199a1b2-c3d4-7e5f-8a6b-7c8d9e0f1a2b", "22d4f1ac"],
    [null, "0199a1b2-c3d4-7e5f-8a6b-7c8d9e0f1a2b", "1ef50fe6"],
    ["ユーザー😀", "clsurveyaaaaaaaaaaaaaaaa1", "7cb9b764"],
  ])("getSurveyKey(%j, %s) = %s, as the server computes it", (userId, surveyId, key) => {
    expect(getSurveyKey(userId, surveyId)).toBe(key);
  });

  test("a key is per user: the same survey has another key for another user", () => {
    expect(getSurveyKey("a", "s1")).not.toBe(getSurveyKey("b", "s1"));
  });

  test("toSurveyKeys reads a state an older SDK stored: displays as objects, responses as survey ids", () => {
    const key = getSurveyKey("u1", "survey-1");
    expect(
      toSurveyKeys("u1", [{ surveyId: "survey-1", createdAt: "2025-01-01T00:00:00Z" }, key, 5, null])
    ).toEqual([key, key]);
    expect(toSurveyKeys("u1", ["survey-1"])).toEqual([key]);
    expect(toSurveyKeys("u1", undefined)).toEqual([]);
  });
});
