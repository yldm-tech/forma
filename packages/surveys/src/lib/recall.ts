import { type TResponseData, type TResponseVariables } from "@forma/types/responses";
import { type TSurveyElement } from "@forma/types/surveys/elements";
import { formatDateWithOrdinal, isValidDateString } from "@/lib/date-time";
import { getLocalizedValue } from "@/lib/i18n";

// Extracts the ID of recall question from a string containing the "recall" pattern.
const extractId = (text: string): string | null => {
  const pattern = /#recall:([A-Za-z0-9_-]+)/;
  const match = text.match(pattern);
  return match?.[1] ?? null;
};

// Extracts the fallback value from a string containing the "fallback" pattern.
// An index scan, not `/fallback:([^#]*)#/`: that pattern is O(N^2) on a long run of `fallback:`
// with no `#` after it, because the engine rescans to the end from every occurrence. Identical
// result — `[^#]*` cannot cross a `#`, so the regex ends at the first `#` after the FIRST
// `fallback:`, and if none follows that one none follows a later one either.
const FALLBACK_MARKER = "fallback:";

const extractFallbackValue = (text: string): string => {
  const markerStart = text.indexOf(FALLBACK_MARKER);
  if (markerStart === -1) return "";

  const valueStart = markerStart + FALLBACK_MARKER.length;
  const valueEnd = text.indexOf("#", valueStart);
  return valueEnd === -1 ? "" : text.slice(valueStart, valueEnd);
};

// Extracts the complete recall information (ID and fallback) from a headline string.
const extractRecallInfo = (headline: string, id?: string): string | null => {
  const idPattern = id ?? "[A-Za-z0-9_-]+";
  const pattern = new RegExp(`#recall:(${idPattern})\\/fallback:([^#]*)#`);
  const match = headline.match(pattern);
  return match ? match[0] : null;
};

export const replaceRecallInfo = (
  text: string,
  responseData: TResponseData,
  variables: TResponseVariables,
  languageCode: string = "en-US"
): string => {
  // Substitution is non-re-entrant: `substituted` holds the text that has already been written and is
  // never scanned again, and each pass only looks at `remaining`, the part after the token just
  // replaced. Rescanning the whole string instead (`modifiedText.replace(...)` in a `while
  // (includes("recall:"))` loop) hangs the browser whenever a recalled value is itself a recall token —
  // a hidden field arriving as `?name=%23recall%3Aname%2Ffallback%3Ax%23`, or a respondent typing one
  // into an open text question — because every pass re-emits the same token and finds it again.
  let substituted = "";
  let remaining = text;

  while (remaining.includes("recall:")) {
    const recallInfo = extractRecallInfo(remaining);
    if (!recallInfo) break; // Exit the loop if no recall info is found

    const recallItemId = extractId(recallInfo);
    if (!recallItemId) return substituted + remaining; // Return the text if no ID could be extracted

    const fallback = extractFallbackValue(recallInfo).replace(/nbsp/g, " ").trim();
    let value: string | null = null;

    // Fetching value from variables based on recallItemId
    if (variables[recallItemId] !== undefined) {
      value = String(variables[recallItemId]) ?? fallback;
    }

    // Fetching value from responseData or attributes based on recallItemId
    if (responseData[recallItemId] !== undefined) {
      value = (responseData[recallItemId] as string) ?? fallback;
    }

    // Additional value formatting if it exists
    if (value) {
      if (isValidDateString(value)) {
        value = formatDateWithOrdinal(new Date(value), languageCode);
      } else if (Array.isArray(value)) {
        value = value.filter((item) => item).join(", "); // Filters out empty values and joins with a comma
      }
    }

    // Replace the recallInfo with the obtained or fallback value, then carry on scanning after it.
    // Sliced rather than `String.replace`, which would also expand `$&` / `$1` in a recalled value
    // into the token that was just matched.
    const matchStart = remaining.indexOf(recallInfo);
    substituted += remaining.slice(0, matchStart) + (value?.toString() || fallback);
    remaining = remaining.slice(matchStart + recallInfo.length);
  }

  return substituted + remaining;
};

export const parseRecallInformation = (
  question: TSurveyElement,
  languageCode: string,
  responseData: TResponseData,
  variables: TResponseVariables
): TSurveyElement => {
  const modifiedQuestion = JSON.parse(JSON.stringify(question));
  // Use getLocalizedValue (falls back to the `default` key) instead of indexing by languageCode
  // directly — a code that isn't a content key (e.g. a legacy SDK language) would otherwise throw.
  if (getLocalizedValue(question.headline, languageCode).includes("recall:")) {
    modifiedQuestion.headline[languageCode] = replaceRecallInfo(
      getLocalizedValue(modifiedQuestion.headline, languageCode),
      responseData,
      variables,
      languageCode
    );
  }
  if (
    question.subheader &&
    getLocalizedValue(question.subheader, languageCode).includes("recall:") &&
    modifiedQuestion.subheader
  ) {
    modifiedQuestion.subheader[languageCode] = replaceRecallInfo(
      getLocalizedValue(modifiedQuestion.subheader, languageCode),
      responseData,
      variables,
      languageCode
    );
  }
  return modifiedQuestion;
};
