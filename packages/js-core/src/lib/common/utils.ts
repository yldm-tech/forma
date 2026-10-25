import { SAMPLING_ID_STORAGE_KEY } from "@/lib/common/constants";
import type {
  TSurveyStyling,
  TUserState,
  TWorkspaceState,
  TWorkspaceStateActionClass,
  TWorkspaceStateSettings,
  TWorkspaceStateSurvey,
  TWorkspaceStyling,
} from "@/types/config";
import type { Result } from "@/types/error";
import { type TActionClassNoCodeConfig, type TActionClassPageUrlRule } from "@/types/survey";

// Helper function to calculate difference in days between two dates
export const diffInDays = (date1: Date, date2: Date): number => {
  const diffTime = Math.abs(date2.getTime() - date1.getTime());
  return Math.floor(diffTime / (1000 * 60 * 60 * 24));
};

export const wrapThrows =
  <T, A extends unknown[]>(fn: (...args: A) => T): ((...args: A) => Result<T>) =>
  (...args: A): Result<T> => {
    try {
      return {
        ok: true,
        data: fn(...args),
      };
    } catch (error) {
      return {
        ok: false,
        error: error as Error,
      };
    }
  };

export const wrapThrowsAsync =
  <T, A extends unknown[]>(fn: (...args: A) => Promise<T>) =>
  async (...args: A): Promise<Result<T>> => {
    try {
      return {
        ok: true,
        data: await fn(...args),
      };
    } catch (error) {
      return {
        ok: false,
        error: error as Error,
      };
    }
  };

/**
 * Detect whether a survey's segment has filters. Handles both the current
 * minimal shape (`{ id, hasFilters }`) and the legacy shape with a `filters`
 * array — older SDKs cached the full segment in localStorage and may still be
 * read back here within the cache window after an SDK upgrade.
 */
export const surveyHasSegmentFilters = (survey: TWorkspaceStateSurvey): boolean => {
  const segment = survey.segment as { hasFilters?: boolean; filters?: unknown[] } | null | undefined;
  if (!segment) return false;
  if (typeof segment.hasFilters === "boolean") return segment.hasFilters;
  return Array.isArray(segment.filters) && segment.filters.length > 0;
};

/**
 * Filters surveys based on the displayOption, recontactDays, and segments
 * @param workspace -  The workspace state
 * @param userState - The user state
 * @returns The filtered surveys
 */

// takes the workspace and user state and returns the filtered surveys
export const filterSurveys = (workspace: TWorkspaceState, userState: TUserState): TWorkspaceStateSurvey[] => {
  const { settings, surveys } = workspace.data;
  const { displays, responses, lastDisplayAt, segments, userId } = userState.data;

  // Function to filter surveys based on displayOption criteria
  let filteredSurveys = surveys.filter((survey: TWorkspaceStateSurvey) => {
    switch (survey.displayOption) {
      case "respondMultiple":
        return true;
      case "displayOnce":
        return displays.filter((display) => display.surveyId === survey.id).length === 0;
      case "displayMultiple":
        return responses.filter((surveyId) => surveyId === survey.id).length === 0;

      case "displaySome":
        if (survey.displayLimit === null) {
          return true;
        }

        // Check if survey response exists, if so, stop here
        if (responses.filter((surveyId) => surveyId === survey.id).length) {
          return false;
        }

        // Otherwise, check if displays length is less than displayLimit
        return displays.filter((display) => display.surveyId === survey.id).length < survey.displayLimit;

      default:
        throw Error("Invalid displayOption");
    }
  });

  // filter surveys that meet the recontactDays criteria
  filteredSurveys = filteredSurveys.filter((survey) => {
    // if no survey was displayed yet, show the survey
    if (!lastDisplayAt) {
      return true;
    }

    // if survey has recontactDays, check if the last display was more than recontactDays ago
    // The previous approach checked the last display for each survey which is why we still have a surveyId in the displays array.
    // TODO: Remove the surveyId from the displays array
    if (survey.recontactDays !== null) {
      return diffInDays(new Date(), new Date(lastDisplayAt)) >= survey.recontactDays;
    }

    // use recontactDays of the workspace if survey does not have recontactDays
    if (settings.recontactDays) {
      return diffInDays(new Date(), new Date(lastDisplayAt)) >= settings.recontactDays;
    }

    // if no recontactDays is set, show the survey

    return true;
  });

  if (!userId) {
    // exclude surveys that have a segment with filters
    return filteredSurveys.filter((survey) => {
      return !surveyHasSegmentFilters(survey);
    });
  }

  if (!segments.length) {
    return [];
  }

  // filter surveys based on segments
  return filteredSurveys.filter((survey) => {
    return survey.segment?.id && segments.includes(survey.segment.id);
  });
};

export const getStyling = (
  settings: TWorkspaceStateSettings,
  survey: TWorkspaceStateSurvey
): TWorkspaceStyling | TSurveyStyling => {
  // allow style overwrite is enabled from the workspace
  if (settings.styling.allowStyleOverwrite) {
    // survey style overwrite is disabled
    if (!survey.styling?.overwriteThemeStyling) {
      return settings.styling;
    }

    // survey style overwrite is enabled
    return survey.styling;
  }

  // allow style overwrite is disabled from the workspace
  return settings.styling;
};

export const getDefaultLanguageCode = (survey: TWorkspaceStateSurvey): string | undefined => {
  const defaultSurveyLanguage = survey.languages.find((surveyLanguage) => {
    return surveyLanguage.default;
  });
  if (defaultSurveyLanguage) return defaultSurveyLanguage.language.code;
};

export const getLanguageCode = (survey: TWorkspaceStateSurvey, language?: string): string | undefined => {
  const availableLanguageCodes = survey.languages.map((surveyLanguage) => surveyLanguage.language.code);
  if (!language) return "default";

  const selectedLanguage = survey.languages.find((surveyLanguage) => {
    return (
      surveyLanguage.language.code.toLowerCase() === language.toLowerCase() ||
      surveyLanguage.language.alias?.toLowerCase() === language.toLowerCase()
    );
  });
  if (selectedLanguage?.default) {
    return "default";
  }
  if (
    !selectedLanguage ||
    !selectedLanguage.enabled ||
    !availableLanguageCodes.includes(selectedLanguage.language.code)
  ) {
    return undefined;
  }
  return selectedLanguage.language.code;
};

export const getSecureRandom = (): number => {
  const u32 = new Uint32Array(1);
  crypto.getRandomValues(u32);
  return u32[0] / 2 ** 32; // Normalized to [0, 1)
};

/**
 * Stable 32-bit hash of a string: FNV-1a over the UTF-16 code units, finished with murmur3's fmix32 so that ids differing only in their last characters still spread across the whole range (plain FNV-1a is weak in its low bits, which is exactly what `% 10000` reads).
 */
export const hashString = (value: string): number => {
  let hash = 0x811c9dc5;
  for (let i = 0; i < value.length; i++) {
    hash ^= value.charCodeAt(i);
    hash = Math.imul(hash, 0x01000193);
  }
  hash ^= hash >>> 16;
  hash = Math.imul(hash, 0x85ebca6b);
  hash ^= hash >>> 13;
  hash = Math.imul(hash, 0xc2b2ae35);
  hash ^= hash >>> 16;
  return hash >>> 0;
};

/** The user's sampling bucket for one survey, in [0, 9999]. The survey id is part of the key so that a 10% survey and another 10% survey do not reach the exact same 10% of users. */
export const getSamplingBucket = (samplingId: string, surveyId: string): number =>
  hashString(`${samplingId}:${surveyId}`) % 10000;

/**
 * Whether this user falls inside a survey's "show to X% of targeted users" sample.
 *
 * Deterministic on purpose: the same user (contact id, or the persisted anonymous id) and survey always land in the same bucket, so the answer is the same on every trigger, page load and session. A per-trigger coin flip would let a 10% survey reach 65% of users who hit its trigger ten times. `displayPercentage` has up to two decimals, hence the 10000 buckets.
 */
export const shouldDisplayBasedOnPercentage = (
  displayPercentage: number,
  samplingId: string,
  surveyId: string
): boolean => getSamplingBucket(samplingId, surveyId) < Math.round(displayPercentage * 100);

// Held for the page's lifetime when localStorage is unavailable (blocked storage, some private modes): sampling then stays stable for this page view at least, instead of re-rolling on every trigger.
let inMemorySamplingId: string | null = null;

const createSamplingId = (): string => {
  const bytes = new Uint32Array(4);
  crypto.getRandomValues(bytes);
  return Array.from(bytes, (n) => n.toString(16).padStart(8, "0")).join("");
};

/**
 * The anonymous visitor's sampling id: random, created once and kept in localStorage under its own key, so `resetConfig()` (logout, a workspace switch, an error state) does not re-roll the visitor's sample. It never leaves the browser.
 */
export const getAnonymousSamplingId = (): string => {
  try {
    const stored = localStorage.getItem(SAMPLING_ID_STORAGE_KEY);
    if (stored) return stored;
    const created = createSamplingId();
    localStorage.setItem(SAMPLING_ID_STORAGE_KEY, created);
    return created;
  } catch {
    inMemorySamplingId ??= createSamplingId();
    return inMemorySamplingId;
  }
};

/** Strips surrounding whitespace and trailing slashes, so an `appUrl` pasted from the address bar (`https://forma.example.com/`) does not produce `//api/...` request paths. */
export const normalizeAppUrl = (appUrl: string): string => appUrl.trim().replace(/\/+$/, "");

export const isNowExpired = (expirationDate: Date): boolean => new Date() >= expirationDate;

export const checkUrlMatch = (
  url: string,
  pageUrlValue: string,
  pageUrlRule: TActionClassPageUrlRule
): boolean => {
  let regex: RegExp;

  switch (pageUrlRule) {
    case "exactMatch":
      return url === pageUrlValue;
    case "contains":
      return url.includes(pageUrlValue);
    case "startsWith":
      return url.startsWith(pageUrlValue);
    case "endsWith":
      return url.endsWith(pageUrlValue);
    case "notMatch":
      return url !== pageUrlValue;
    case "notContains":
      return !url.includes(pageUrlValue);
    case "matchesRegex":
      try {
        regex = new RegExp(pageUrlValue);
      } catch {
        // edge case: fail closed if the regex expression is invalid
        return false;
      }

      return regex.test(url);
    default:
      return false;
  }
};

export const handleUrlFilters = (
  urlFilters: TActionClassNoCodeConfig["urlFilters"],
  connector: "or" | "and" = "or"
): boolean => {
  if (urlFilters.length === 0) {
    return true;
  }

  const windowUrl = window.location.href;

  if (connector === "and") {
    const isMatch = urlFilters.every((filter) => {
      const match = checkUrlMatch(windowUrl, filter.value, filter.rule);
      return match;
    });
    return isMatch;
  }

  const isMatch = urlFilters.some((filter) => {
    const match = checkUrlMatch(windowUrl, filter.value, filter.rule);
    return match;
  });

  return isMatch;
};

const INTERACTIVE_ANCESTOR_SELECTOR = "button, a, [role='button']";

const normalizeText = (text: string | null | undefined): string => (text ?? "").replace(/\s+/g, " ").trim();

/**
 * Whether a click matches a click action's "Inner Text" value.
 *
 * The editor describes the field as the text of the clicked button, so the comparison is on whitespace-normalized `textContent`: of the clicked element, of its nearest button / link / `role=button` ancestor (a click on the `<svg>` inside `<button><svg/> Buy now</button>` lands on the svg), and of the element the CSS selector resolved to. Exact `innerHTML` equality on the resolved element is kept as a fallback, because actions saved under the old behaviour may store markup that a text comparison would never equal.
 */
const matchesClickText = (
  targetElement: HTMLElement,
  matchedElement: HTMLElement,
  value: string
): boolean => {
  if (matchedElement.innerHTML === value) return true;

  const expected = normalizeText(value);
  if (!expected) return false;

  const interactiveAncestor = targetElement.closest(INTERACTIVE_ANCESTOR_SELECTOR);

  return [targetElement, interactiveAncestor, matchedElement].some(
    (element) => element !== null && normalizeText(element.textContent) === expected
  );
};

export const evaluateNoCodeConfigClick = (
  targetElement: HTMLElement,
  action: TWorkspaceStateActionClass
): boolean => {
  if (action.noCodeConfig?.type !== "click") return false;

  const innerHtml = action.noCodeConfig.elementSelector.innerHtml;
  const cssSelector = action.noCodeConfig.elementSelector.cssSelector;
  const urlFilters = action.noCodeConfig.urlFilters;

  if (!innerHtml && !cssSelector) return false;

  // Resolve the element to test: prefer the direct click target, but walk up to
  // the nearest ancestor that matches the CSS selector (event delegation for nested markup,
  // e.g. <svg> or <span> inside a <button class="my-btn">).
  let matchedElement: HTMLElement = targetElement;

  if (cssSelector) {
    let matchesDirectly = false;
    try {
      matchesDirectly = targetElement.matches(cssSelector);
    } catch {
      matchesDirectly = false;
    }
    if (!matchesDirectly) {
      // `closest` throws the same SyntaxError as `matches` on a selector the visitor's browser cannot
      // parse, and an unparseable selector always reaches it because the catch above leaves
      // `matchesDirectly` false. Unguarded it would abort the caller's loop over every click action.
      let ancestor: Element | null = null;
      try {
        ancestor = targetElement.closest(cssSelector);
      } catch {
        return false;
      }
      if (!ancestor) return false;
      matchedElement = ancestor as HTMLElement;
    }
  }

  if (innerHtml && !matchesClickText(targetElement, matchedElement, innerHtml)) return false;

  const connector = action.noCodeConfig.urlFiltersConnector ?? "or";
  const isValidUrl = handleUrlFilters(urlFilters, connector);

  if (!isValidUrl) return false;

  return true;
};

export const getIsDebug = (): boolean => window.location.search.includes("formaDebug=true");
