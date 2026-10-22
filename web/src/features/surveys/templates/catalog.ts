import type { TFunction } from "i18next";
import type { SurveyTemplate, TemplateChannel, TemplateIndustry, TemplateRole } from "../api";

// How the template gallery filters and labels the catalog, forma2's modules/survey/components/template-list (index.tsx, lib/utils.ts, components/template-tags.tsx).

/** The gallery's three filters, [channel, industry, role]; null on an axis matches every template. */
export type TemplateFilter = [TemplateChannel | null, TemplateIndustry | null, TemplateRole | null];

export const noTemplateFilter: TemplateFilter = [null, null, null];

export const channelOptions = (t: TFunction): { value: TemplateChannel; label: string }[] => [
  { value: "website", label: t("common.website_survey") },
  { value: "app", label: t("common.app_survey") },
  { value: "link", label: t("common.link_survey") },
];

export const industryOptions = (t: TFunction): { value: TemplateIndustry; label: string }[] => [
  { value: "eCommerce", label: t("common.e_commerce") },
  { value: "saas", label: t("common.saas") },
  { value: "other", label: t("common.other") },
];

export const roleOptions = (t: TFunction): { value: TemplateRole; label: string }[] => [
  { value: "productManager", label: t("common.product_manager") },
  { value: "marketing", label: t("common.marketing") },
  { value: "customerSuccess", label: t("common.customer_success") },
  { value: "peopleManager", label: t("common.people_manager") },
  { value: "sales", label: t("common.sales") },
];

/** forma2's filteredTemplates: a search matches the name, case-insensitively, and replaces the filters; otherwise a template must match every axis that is set. */
export const filterTemplates = (
  templates: SurveyTemplate[],
  search: string,
  [channel, industry, role]: TemplateFilter
): SurveyTemplate[] =>
  templates.filter((template) => {
    if (search) return template.name.toLowerCase().includes(search.toLowerCase());
    return (
      (channel === null || (template.channels?.includes(channel) ?? false)) &&
      (industry === null || (template.industries?.includes(industry) ?? false)) &&
      (role === null || template.role === role)
    );
  });

/** The channel tag of a card: the one channel, two joined by "or" without their "Survey" suffix, or "All channels" for three. */
export const channelTag = (channels: TemplateChannel[] | undefined, t: TFunction): string | undefined => {
  if (!channels) return undefined;
  const options = channelOptions(t);
  const labels = channels
    .map((channel) => options.find((option) => option.value === channel)?.label)
    .filter((label): label is string => !!label)
    .sort((a, b) => a.localeCompare(b));
  switch (channels.length) {
    case 1:
      return labels[0];
    case 2:
      return labels.map((label) => label.replace(" Survey", "")).join(` ${t("common.or")} `);
    case 3:
      return t("workspace.surveys.templates.all_channels");
    default:
      return undefined;
  }
};

/** The industry tag of a card: the filtered industry once one is chosen, otherwise the template's one industry or "Multiple industries". */
export const industryTag = (
  industries: TemplateIndustry[] | undefined,
  selectedIndustry: TemplateIndustry | null,
  t: TFunction
): string | undefined => {
  const options = industryOptions(t);
  if (selectedIndustry !== null) {
    const selected = options.find((option) => option.value === selectedIndustry);
    if (selected) return selected.label;
  }
  if (!industries || industries.length === 0) return undefined;
  if (industries.length > 1) return t("workspace.surveys.templates.multiple_industries");
  return options.find((option) => option.value === industries[0])?.label;
};

/** Whether any block of the template branches with logic. */
export const usesBranchingLogic = (template: SurveyTemplate): boolean =>
  (template.preset.blocks ?? []).some((block) => Array.isArray(block.logic) && block.logic.length > 0);
