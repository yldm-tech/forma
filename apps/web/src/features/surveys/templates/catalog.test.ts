import type { TFunction } from "i18next";
import { describe, expect, test } from "vite-plus/test";
import type { SurveyTemplate } from "../api";
import {
  channelOptions,
  channelTag,
  filterTemplates,
  industryOptions,
  industryTag,
  noTemplateFilter,
  roleOptions,
  usesBranchingLogic,
} from "./catalog";

const labels: Record<string, string> = {
  "common.website_survey": "Website Survey",
  "common.app_survey": "App Survey",
  "common.link_survey": "Link Survey",
  "common.or": "or",
  "common.saas": "SaaS",
  "common.e_commerce": "E-Commerce",
};
const t = ((key: string) => labels[key] ?? key) as unknown as TFunction;

const template = (overrides: Partial<SurveyTemplate>): SurveyTemplate => ({
  id: "t",
  name: "Template",
  description: "",
  preset: { blocks: [] },
  ...overrides,
});

const nps = template({
  id: "nps",
  name: "Net Promoter Score (NPS)",
  role: "customerSuccess",
  channels: ["link", "app", "website"],
  industries: ["saas", "eCommerce", "other"],
});
const churn = template({
  id: "churn",
  name: "Churn Survey",
  role: "sales",
  channels: ["app", "link"],
  industries: ["saas"],
});
const cart = template({
  id: "cart",
  name: "Abandoned Cart",
  role: "marketing",
  channels: ["website"],
  industries: ["eCommerce"],
});
const catalog = [nps, churn, cart];

// forma2's modules/survey/components/template-list/lib/utils.test.ts, and the filtering of its index.tsx.
describe("filter options", () => {
  test("are forma2's channels, industries and roles, in its order", () => {
    const keys = ((key: string) => key) as unknown as TFunction;
    expect(channelOptions(keys)).toEqual([
      { value: "website", label: "common.website_survey" },
      { value: "app", label: "common.app_survey" },
      { value: "link", label: "common.link_survey" },
    ]);
    expect(industryOptions(keys)).toEqual([
      { value: "eCommerce", label: "common.e_commerce" },
      { value: "saas", label: "common.saas" },
      { value: "other", label: "common.other" },
    ]);
    expect(roleOptions(keys)).toEqual([
      { value: "productManager", label: "common.product_manager" },
      { value: "marketing", label: "common.marketing" },
      { value: "customerSuccess", label: "common.customer_success" },
      { value: "peopleManager", label: "common.people_manager" },
      { value: "sales", label: "common.sales" },
    ]);
  });
});

describe("filterTemplates", () => {
  test("lists everything without a filter", () => {
    expect(filterTemplates(catalog, "", noTemplateFilter)).toEqual(catalog);
  });

  test("a template must match every axis that is set", () => {
    expect(filterTemplates(catalog, "", ["app", null, null]).map((x) => x.id)).toEqual(["nps", "churn"]);
    expect(filterTemplates(catalog, "", ["app", "saas", "sales"]).map((x) => x.id)).toEqual(["churn"]);
    expect(filterTemplates(catalog, "", [null, "eCommerce", null]).map((x) => x.id)).toEqual(["nps", "cart"]);
    expect(filterTemplates(catalog, "", [null, null, "peopleManager"])).toEqual([]);
  });

  test("a template without channels or industries matches only when that axis is unset", () => {
    const bare = template({ id: "bare", role: "sales" });
    expect(filterTemplates([bare], "", [null, null, "sales"])).toEqual([bare]);
    expect(filterTemplates([bare], "", ["link", null, null])).toEqual([]);
  });

  test("a search matches names case-insensitively and replaces the filters", () => {
    expect(filterTemplates(catalog, "CART", ["app", "saas", null]).map((x) => x.id)).toEqual(["cart"]);
  });
});

describe("channelTag", () => {
  test("names one channel, joins two without their suffix, and says all for three", () => {
    expect(channelTag(["website"], t)).toBe("Website Survey");
    expect(channelTag(["link", "app"], t)).toBe("App or Link");
    expect(channelTag(["link", "app", "website"], t)).toBe("workspace.surveys.templates.all_channels");
    expect(channelTag(undefined, t)).toBeUndefined();
  });
});

describe("industryTag", () => {
  test("names the chosen industry, else the template's one industry or several", () => {
    expect(industryTag(["saas", "other"], "eCommerce", t)).toBe("E-Commerce");
    expect(industryTag(["saas"], null, t)).toBe("SaaS");
    expect(industryTag(["saas", "other"], null, t)).toBe("workspace.surveys.templates.multiple_industries");
    expect(industryTag([], null, t)).toBeUndefined();
    expect(industryTag(undefined, null, t)).toBeUndefined();
  });
});

describe("usesBranchingLogic", () => {
  test("is true when any block has logic", () => {
    expect(
      usesBranchingLogic(template({ preset: { blocks: [{ logic: [] }, { logic: [{ id: "l" }] }] } }))
    ).toBe(true);
    expect(usesBranchingLogic(template({ preset: { blocks: [{ logic: [] }, {}] } }))).toBe(false);
    expect(usesBranchingLogic(template({ preset: {} }))).toBe(false);
  });
});
