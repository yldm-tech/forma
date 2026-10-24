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

// The field rules of ZActionClassInput, each failure reported on its own path with a translated message.
const ZName = z.string().trim().min(1);
const ZKey = z.string().trim().min(1).nullable();
const ZUrlFilterValue = z.string().trim().min(1);
const ZTimeInSeconds = z.number().int().min(1).max(3600);

/**
 * The form's errors by field path ("name", "key", "noCodeConfig.urlFilters.0.value", ...): ZActionClassInput's rules, then forma2's form refinements: a name or key another action of the workspace has, an invalid CSS selector, and a regex URL filter that does not compile. Every message is translated and names fields by their labels in the form.
 */
export const actionFormErrors = (
  values: ActionFormValues,
  otherNames: string[],
  otherKeys: string[],
  t: TFunction
): Record<string, string> => {
  const errors: Record<string, string> = {};
  if (!ZName.safeParse(values.name).success) errors.name = t("workspace.actions.name_required");
  else if (otherNames.includes(values.name))
    errors.name = t("workspace.actions.action_with_name_already_exists", { name: values.name });

  if (values.type === "code") {
    if (!ZKey.safeParse(values.key).success) errors.key = t("workspace.actions.key_required");
    else if (values.key && otherKeys.includes(values.key))
      errors.key = t("workspace.actions.action_with_key_already_exists", { key: values.key });
    return errors;
  }

  const config = values.noCodeConfig;
  if (!config) return errors;
  config.urlFilters.forEach((filter, index) => {
    const path = `noCodeConfig.urlFilters.${index}.value`;
    if (!ZUrlFilterValue.safeParse(filter.value).success)
      errors[path] = t("workspace.actions.url_value_required");
    else if (!isValidActionClassUrlFilterValue(filter.value.trim(), filter.rule))
      errors[path] = t("workspace.actions.invalid_url");
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
      errors["noCodeConfig.elementSelector"] = t("workspace.actions.css_selector_or_inner_text_required");
    else if (cssSelector && !isValidCssSelector(cssSelector))
      errors["noCodeConfig.elementSelector.cssSelector"] = t("workspace.actions.invalid_css_selector");
  }
  if (config.type === "pageDwell") {
    if (!ZTimeInSeconds.safeParse(config.timeInSeconds).success)
      errors["noCodeConfig.timeInSeconds"] = t("workspace.actions.time_in_seconds_range", {
        min: 1,
        max: 3600,
      });
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

/** The trigger column of the actions list: what kind of trigger an action is, what it watches (the selector or text clicked, the code key, the time on page) and on which pages. */
export const actionTriggerSummary = (
  actionClass: Pick<ActionClass, "type" | "key" | "noCodeConfig">,
  t: TFunction
): { kind: string; target: string; pages: string } => {
  if (actionClass.type === "code")
    return { kind: t("common.code"), target: actionClass.key ?? "", pages: "" };
  if (actionClass.type === "automatic")
    return { kind: t("workspace.actions.trigger_automatic"), target: "", pages: "" };
  const config = actionClass.noCodeConfig;
  const kinds: Record<NoCodeType, string> = {
    click: t("workspace.actions.click"),
    pageView: t("workspace.actions.page_view"),
    exitIntent: t("workspace.actions.exit_intent"),
    fiftyPercentScroll: t("workspace.actions.fifty_percent_scroll"),
    pageDwell: t("workspace.actions.time_on_page"),
  };
  const kind = kinds[config?.type ?? "click"] ?? t("common.no_code");
  if (!config) return { kind, target: "", pages: "" };
  const target =
    config.type === "click"
      ? config.elementSelector?.cssSelector?.trim() ||
        (config.elementSelector?.innerHtml?.trim() ? `“${config.elementSelector.innerHtml.trim()}”` : "")
      : config.type === "pageDwell" && config.timeInSeconds
        ? t("workspace.actions.time_in_seconds_with_unit", { seconds: config.timeInSeconds })
        : "";
  // Each filter keeps its rule, so an excluding rule ("does not contain /admin") never reads as if it included the page, and the filters are joined by their own connector.
  const connector = t(
    config.urlFiltersConnector === "and" ? "workspace.actions.and" : "workspace.actions.or"
  );
  const pages =
    config.urlFilters.length === 0
      ? t("workspace.actions.on_all_pages")
      : config.urlFilters
          .map((filter) =>
            t("workspace.actions.trigger_url_rule", { rule: filter.rule, value: filter.value })
          )
          .join(` ${connector} `);
  return { kind, target, pages };
};

/** The translated name of an action's type, rather than the raw enum ("noCode"). */
export const actionTypeLabel = (type: string, t: TFunction): string =>
  type === "code"
    ? t("common.code")
    : type === "noCode"
      ? t("common.no_code")
      : t("workspace.actions.trigger_automatic");
