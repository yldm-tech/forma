import { type TActionClassPageUrlRule, isValidActionClassUrlFilterValue } from "@forma/types/action-classes";
import type { TFunction } from "i18next";
import { z } from "zod";

// forma2's action class form rules (packages/types/action-classes.ts, modules/survey/editor/lib/action-utils.ts and action-builder.ts, lib/utils/url.ts).

export type NoCodeType = "click" | "pageView" | "exitIntent" | "fiftyPercentScroll" | "pageDwell";

export interface UrlFilter {
  value: string;
  rule: TActionClassPageUrlRule;
}

export interface NoCodeConfig {
  type: NoCodeType;
  urlFilters: UrlFilter[];
  urlFiltersConnector?: "or" | "and";
  /** Click actions only; a selector left undefined is switched off. */
  elementSelector?: { cssSelector?: string; innerHtml?: string };
  /** Time-on-page actions only. */
  timeInSeconds?: number;
}

/** An action class as the server stores and returns it. */
export interface ActionClass {
  id: string;
  createdAt: string;
  updatedAt: string;
  name: string;
  description: string | null;
  type: "code" | "noCode" | "automatic";
  key: string | null;
  noCodeConfig: NoCodeConfig | null;
  workspaceId: string;
}

/** The values of the create and settings forms. */
export interface ActionFormValues {
  name: string;
  description: string | null;
  type: "code" | "noCode";
  key: string | null;
  noCodeConfig: NoCodeConfig | null;
}

/** The body of a create or update request, as forma2's buildActionObject shaped it. */
export type ActionInput =
  | { name: string; description: string | null; type: "code"; key: string | null }
  | { name: string; description: string | null; type: "noCode"; noCodeConfig: NoCodeConfig | null };

/** The create form's starting values: a click action with no selector and no page filter. */
export const NEW_ACTION_VALUES: ActionFormValues = {
  name: "",
  description: "",
  type: "noCode",
  key: null,
  noCodeConfig: {
    type: "click",
    elementSelector: { cssSelector: undefined, innerHtml: undefined },
    urlFilters: [],
  },
};

export const formValuesOf = (actionClass: ActionClass): ActionFormValues => ({
  name: actionClass.name,
  description: actionClass.description,
  type: actionClass.type === "code" ? "code" : "noCode",
  key: actionClass.key,
  noCodeConfig: actionClass.noCodeConfig,
});

/** forma2's isValidCssSelector: a selector the browser's query engine accepts. */
export const isValidCssSelector = (selector?: string): boolean => {
  if (!selector) return false;
  try {
    document.createElement("div").querySelector(selector);
    return true;
  } catch {
    return false;
  }
};

/** The config a time-on-page or click action needs, from the shared one the type switch keeps. */
export const noCodeConfigForType = (current: NoCodeConfig | null, type: NoCodeType): NoCodeConfig => {
  const base = {
    type,
    urlFilters: current?.urlFilters ?? [],
    urlFiltersConnector: current?.urlFiltersConnector,
  };
  if (type === "click") return { ...base, elementSelector: current?.elementSelector ?? {} };
  if (type === "pageDwell") return { ...base, timeInSeconds: current?.timeInSeconds };
  return base;
};

// The field rules of ZActionClassInput, so each failure keeps its own message and path.
const ZName = z.string().trim().min(1, { error: "Name must be at least 1 character long" });
const ZKey = z.string().trim().min(1).nullable();
const ZUrlFilterValue = z.string().trim().min(1, { error: "Value must contain at least 1 character" });
const ZTimeInSeconds = z.number().int().min(1).max(3600);

const firstIssue = (result: { error?: { issues: { message: string }[] } }) =>
  result.error?.issues[0]?.message;

/**
 * The form's errors by field path ("name", "key", "noCodeConfig.urlFilters.0.value", ...): ZActionClassInput's rules with their messages, then forma2's form refinements: a name or key another action of the workspace has, an invalid CSS selector, and a regex URL filter that does not compile.
 */
export const actionFormErrors = (
  values: ActionFormValues,
  otherNames: string[],
  otherKeys: string[],
  t: TFunction
): Record<string, string> => {
  const errors: Record<string, string> = {};
  const nameIssue = firstIssue(ZName.safeParse(values.name));
  if (nameIssue) errors.name = nameIssue;
  else if (otherNames.includes(values.name))
    errors.name = t("workspace.actions.action_with_name_already_exists", { name: values.name });

  if (values.type === "code") {
    const keyIssue = firstIssue(ZKey.safeParse(values.key));
    if (keyIssue) errors.key = keyIssue;
    else if (values.key && otherKeys.includes(values.key))
      errors.key = t("workspace.actions.action_with_key_already_exists", { key: values.key });
    return errors;
  }

  const config = values.noCodeConfig;
  if (!config) return errors;
  config.urlFilters.forEach((filter, index) => {
    const path = `noCodeConfig.urlFilters.${index}.value`;
    const valueIssue = firstIssue(ZUrlFilterValue.safeParse(filter.value));
    if (valueIssue) errors[path] = valueIssue;
    else if (!isValidActionClassUrlFilterValue(filter.value.trim(), filter.rule))
      errors[path] = "Please enter a valid URL (e.g., https://example.com)";
    else if (filter.rule === "matchesRegex") {
      try {
        new RegExp(filter.value);
      } catch {
        errors[path] = t("common.invalid_regex");
      }
    }
  });
  if (config.type === "click") {
    const cssSelector = config.elementSelector?.cssSelector?.trim();
    const innerHtml = config.elementSelector?.innerHtml?.trim();
    if (!cssSelector && !innerHtml)
      errors["noCodeConfig.elementSelector"] = "Either cssSelector or innerHtml must be provided";
    else if (cssSelector && !isValidCssSelector(cssSelector))
      errors["noCodeConfig.elementSelector.cssSelector"] = t("workspace.actions.invalid_css_selector");
  }
  if (config.type === "pageDwell") {
    const secondsIssue = firstIssue(ZTimeInSeconds.safeParse(config.timeInSeconds));
    if (secondsIssue) errors["noCodeConfig.timeInSeconds"] = secondsIssue;
  }
  return errors;
};

/** forma2's buildActionObject: the trimmed name, and the key or no-code config the type uses, with only the parts of the config that apply. */
export const buildActionInput = (values: ActionFormValues): ActionInput => {
  const name = values.name.trim();
  if (values.type === "code") return { name, description: values.description, type: "code", key: values.key };
  const config = values.noCodeConfig;
  return {
    name,
    description: values.description,
    type: "noCode",
    noCodeConfig: config && noCodeConfigForType(config, config.type),
  };
};

/** forma2's testURLmatch: whether a URL passes one page filter. A regex that does not compile throws the translated message. */
export const testUrlMatch = (
  testUrl: string,
  value: string,
  rule: TActionClassPageUrlRule,
  t: TFunction
): boolean => {
  switch (rule) {
    case "exactMatch":
      return testUrl === value;
    case "contains":
      return testUrl.includes(value);
    case "startsWith":
      return testUrl.startsWith(value);
    case "endsWith":
      return testUrl.endsWith(value);
    case "notMatch":
      return testUrl !== value;
    case "notContains":
      return !testUrl.includes(value);
    case "matchesRegex": {
      let regex: RegExp;
      try {
        regex = new RegExp(value);
      } catch {
        throw new Error(t("common.invalid_regex"));
      }
      return regex.test(testUrl);
    }
    default:
      throw new Error(t("common.invalid_match_type"));
  }
};

/** Whether a URL passes the page filters joined by their connector ("or" unless set). */
export const urlPassesFilters = (testUrl: string, config: NoCodeConfig, t: TFunction): boolean => {
  const passes = (filter: UrlFilter) => testUrlMatch(testUrl, filter.value, filter.rule, t);
  return config.urlFiltersConnector === "and"
    ? config.urlFilters.every(passes)
    : config.urlFilters.some(passes);
};
