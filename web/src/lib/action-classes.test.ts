import { describe, expect, test } from "vite-plus/test";
import {
  type ActionFormValues,
  NEW_ACTION_VALUES,
  actionFormErrors,
  buildActionInput,
  noCodeConfigForType,
  testUrlMatch,
  urlPassesFilters,
} from "./action-classes";
import { initI18n } from "./i18n";

const { t } = await initI18n("en-US");

const code = (overrides: Partial<ActionFormValues> = {}): ActionFormValues => ({
  name: "Clicked buy",
  description: null,
  type: "code",
  key: "buy",
  noCodeConfig: null,
  ...overrides,
});

const noCode = (config: ActionFormValues["noCodeConfig"]): ActionFormValues => ({
  ...NEW_ACTION_VALUES,
  name: "Visited pricing",
  noCodeConfig: config,
});

describe("actionFormErrors", () => {
  test("a name is required and unique in the workspace", () => {
    expect(actionFormErrors(code({ name: "  " }), [], [], t)).toEqual({
      name: "Name must be at least 1 character long",
    });
    expect(actionFormErrors(code(), ["Clicked buy"], [], t)).toEqual({
      name: "Action with name Clicked buy already exists",
    });
  });

  test("a code action's key is unique and not blank", () => {
    expect(actionFormErrors(code(), [], ["buy"], t)).toEqual({ key: "Action with key buy already exists" });
    expect(Object.keys(actionFormErrors(code({ key: " " }), [], [], t))).toEqual(["key"]);
  });

  test("page filters need a value, a URL for URL-like rules and a regex that compiles", () => {
    const errors = actionFormErrors(
      noCode({
        type: "pageView",
        urlFilters: [
          { rule: "contains", value: "" },
          { rule: "exactMatch", value: "pricing" },
          { rule: "startsWith", value: "/pricing" },
          { rule: "matchesRegex", value: "([" },
        ],
      }),
      [],
      [],
      t
    );
    expect(errors).toEqual({
      "noCodeConfig.urlFilters.0.value": "Value must contain at least 1 character",
      "noCodeConfig.urlFilters.1.value": "Please enter a valid URL (e.g., https://example.com)",
      "noCodeConfig.urlFilters.3.value": "Please use a valid regular expression.",
    });
  });

  test("a click action needs a selector or an inner text", () => {
    expect(
      actionFormErrors(noCode({ type: "click", urlFilters: [], elementSelector: {} }), [], [], t)
    ).toEqual({
      "noCodeConfig.elementSelector": "Either cssSelector or innerHtml must be provided",
    });
    expect(
      actionFormErrors(
        noCode({ type: "click", urlFilters: [], elementSelector: { innerHtml: "Buy" } }),
        [],
        [],
        t
      )
    ).toEqual({});
  });

  test("time on page is a whole number of seconds up to an hour", () => {
    const dwell = (timeInSeconds?: number) =>
      actionFormErrors(noCode({ type: "pageDwell", urlFilters: [], timeInSeconds }), [], [], t);
    expect(dwell(10)).toEqual({});
    expect(Object.keys(dwell(0))).toEqual(["noCodeConfig.timeInSeconds"]);
    expect(Object.keys(dwell(3601))).toEqual(["noCodeConfig.timeInSeconds"]);
    expect(Object.keys(dwell(undefined))).toEqual(["noCodeConfig.timeInSeconds"]);
  });
});

test("buildActionInput trims the name and keeps only what the type uses", () => {
  expect(buildActionInput(code({ name: " Clicked buy " }))).toEqual({
    name: "Clicked buy",
    description: null,
    type: "code",
    key: "buy",
  });
  const input = buildActionInput(
    noCode({ type: "pageView", urlFilters: [], elementSelector: { innerHtml: "x" }, timeInSeconds: 5 })
  );
  expect(input).toEqual({
    name: "Visited pricing",
    description: "",
    type: "noCode",
    noCodeConfig: { type: "pageView", urlFilters: [], urlFiltersConnector: undefined },
  });
});

test("switching the no-code type keeps the page filters", () => {
  const config = noCodeConfigForType(
    {
      type: "click",
      urlFilters: [{ rule: "contains", value: "/a" }],
      elementSelector: { cssSelector: ".x" },
    },
    "pageDwell"
  );
  expect(config).toEqual({
    type: "pageDwell",
    urlFilters: [{ rule: "contains", value: "/a" }],
    urlFiltersConnector: undefined,
    timeInSeconds: undefined,
  });
});

test("URL matching follows each rule and the connector", () => {
  expect(testUrlMatch("https://a.com/x", "https://a.com/x", "exactMatch", t)).toBe(true);
  expect(testUrlMatch("https://a.com/x", "/y", "notContains", t)).toBe(true);
  expect(testUrlMatch("https://a.com/x", "^https://a\\.com", "matchesRegex", t)).toBe(true);
  expect(() => testUrlMatch("x", "([", "matchesRegex", t)).toThrow("Please use a valid regular expression.");
  const filters = [
    { rule: "contains" as const, value: "/x" },
    { rule: "endsWith" as const, value: "/y" },
  ];
  expect(urlPassesFilters("https://a.com/x", { type: "pageView", urlFilters: filters }, t)).toBe(true);
  expect(
    urlPassesFilters(
      "https://a.com/x",
      { type: "pageView", urlFilters: filters, urlFiltersConnector: "and" },
      t
    )
  ).toBe(false);
});
