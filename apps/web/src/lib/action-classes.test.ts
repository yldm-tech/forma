import { describe, expect, test } from "vite-plus/test";
import {
  type ActionFormValues,
  NEW_ACTION_VALUES,
  actionFormErrors,
  actionTriggerSummary,
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
      name: "Enter a name for the action.",
    });
    expect(actionFormErrors(code(), ["Clicked buy"], [], t)).toEqual({
      name: "Action with name Clicked buy already exists",
    });
  });

  test("a code action's key is unique and not blank", () => {
    expect(actionFormErrors(code(), [], ["buy"], t)).toEqual({ key: "Action with key buy already exists" });
    expect(actionFormErrors(code({ key: " " }), [], [], t)).toEqual({ key: "Enter a key for the action." });
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
      "noCodeConfig.urlFilters.0.value": "Enter a value for this page filter.",
      "noCodeConfig.urlFilters.1.value": "Enter a valid URL (e.g. https://example.com).",
      "noCodeConfig.urlFilters.3.value": "Please use a valid regular expression.",
    });
  });

  test("a click action needs a selector or an inner text", () => {
    expect(
      actionFormErrors(noCode({ type: "click", urlFilters: [], elementSelector: {} }), [], [], t)
    ).toEqual({
      "noCodeConfig.elementSelector": "Enter a CSS selector, an inner text, or both.",
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
    expect(dwell(0)).toEqual({
      "noCodeConfig.timeInSeconds": "Enter a whole number of seconds from 1 to 3600.",
    });
    expect(Object.keys(dwell(3601))).toEqual(["noCodeConfig.timeInSeconds"]);
    expect(Object.keys(dwell(undefined))).toEqual(["noCodeConfig.timeInSeconds"]);
  });

  test("every message is translated, so a Japanese form shows no English", async () => {
    const ja = await initI18n("ja-JP");
    const errors = {
      ...actionFormErrors(code({ name: " ", key: " " }), [], [], ja.t),
      ...actionFormErrors(
        noCode({
          type: "click",
          urlFilters: [
            { rule: "contains", value: "" },
            { rule: "exactMatch", value: "pricing" },
          ],
          elementSelector: {},
        }),
        [],
        [],
        ja.t
      ),
      ...actionFormErrors(noCode({ type: "pageDwell", urlFilters: [], timeInSeconds: 0 }), [], [], ja.t),
    };
    expect(Object.keys(errors).sort()).toEqual([
      "key",
      "name",
      "noCodeConfig.elementSelector",
      "noCodeConfig.timeInSeconds",
      "noCodeConfig.urlFilters.0.value",
      "noCodeConfig.urlFilters.1.value",
    ]);
    for (const message of Object.values(errors)) expect(message).toMatch(/[\u3040-\u30ff\u4e00-\u9fff]/);
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

describe("actionTriggerSummary", () => {
  test("names the trigger kind and what it watches, on which pages", () => {
    expect(actionTriggerSummary({ type: "code", key: "checkout", noCodeConfig: null }, t)).toEqual({
      kind: "Code",
      detail: "checkout",
    });
    expect(
      actionTriggerSummary(
        {
          type: "noCode",
          key: null,
          noCodeConfig: {
            type: "click",
            elementSelector: { cssSelector: "#upgrade" },
            urlFilters: [{ rule: "contains", value: "/pricing" }],
          },
        },
        t
      )
    ).toEqual({ kind: "Click", detail: "#upgrade · on /pricing" });
    expect(
      actionTriggerSummary(
        {
          type: "noCode",
          key: null,
          noCodeConfig: { type: "click", elementSelector: { innerHtml: "Buy now" }, urlFilters: [] },
        },
        t
      )
    ).toEqual({ kind: "Click", detail: "“Buy now” · On all pages" });
    expect(
      actionTriggerSummary(
        { type: "noCode", key: null, noCodeConfig: { type: "pageDwell", timeInSeconds: 30, urlFilters: [] } },
        t
      )
    ).toEqual({ kind: "Time on Page", detail: "30s · On all pages" });
    expect(
      actionTriggerSummary(
        { type: "noCode", key: null, noCodeConfig: { type: "exitIntent", urlFilters: [] } },
        t
      ).kind
    ).toBe("Exit Intent");
    expect(actionTriggerSummary({ type: "automatic", key: null, noCodeConfig: null }, t)).toEqual({
      kind: "Automatic",
      detail: "",
    });
  });
});
