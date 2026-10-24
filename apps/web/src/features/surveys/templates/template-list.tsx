import { useQuery } from "@tanstack/react-query";
import { PlusCircleIcon, SplitIcon } from "lucide-react";
import { type ReactNode, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../components/ui/button";
import { LoadingBlock } from "../../../components/ui/page";
import { cn } from "../../../lib/cn";
import { errorMessage } from "../../../lib/errors";
import { CUSTOM_TEMPLATE_ID, type SurveyTemplate, type TemplateRole, surveyTemplatesQuery } from "../api";
import {
  type TemplateFilter,
  channelOptions,
  channelTag,
  filterTemplates,
  industryOptions,
  industryTag,
  noTemplateFilter,
  roleOptions,
  usesBranchingLogic,
} from "./catalog";
import { useCreateSurveyFromTemplate } from "./use-create-survey";

const roleStyling: Record<TemplateRole, string> = {
  productManager: "border-blue-300 bg-blue-50 text-blue-500",
  marketing: "border-orange-300 bg-orange-50 text-orange-500",
  sales: "border-emerald-300 bg-emerald-50 text-emerald-500",
  customerSuccess: "border-violet-300 bg-violet-50 text-violet-500",
  peopleManager: "border-pink-300 bg-pink-50 text-pink-500",
};

const chipClass = (active: boolean) =>
  cn(
    active
      ? "bg-slate-800 font-semibold text-white"
      : "bg-white text-slate-700 hover:bg-slate-100 focus:bg-slate-100 focus:outline-hidden",
    "rounded-sm border border-slate-800 px-2 py-1 text-xs transition-all duration-150"
  );

/** One axis of the gallery's filters: "All ..." and each value, one of them chosen. */
const FilterRow = <T extends string>({
  allLabel,
  options,
  value,
  onChange,
}: {
  allLabel: string;
  options: { value: T; label: string }[];
  value: T | null;
  onChange: (value: T | null) => void;
}) => (
  <div role="group" aria-label={allLabel} className="mt-2 flex flex-wrap gap-1">
    <button
      type="button"
      aria-pressed={value === null}
      onClick={() => onChange(null)}
      className={chipClass(value === null)}>
      {allLabel}
    </button>
    {options.map((option) => (
      <button
        key={option.value}
        type="button"
        aria-pressed={value === option.value}
        onClick={() => onChange(option.value)}
        className={chipClass(value === option.value)}>
        {option.label}
      </button>
    ))}
  </div>
);

const TemplateFilters = ({
  selected: [channel, industry, role],
  setSelected,
}: {
  selected: TemplateFilter;
  setSelected: (filter: TemplateFilter) => void;
}) => {
  const { t } = useTranslation();
  return (
    <div className="mb-6">
      <FilterRow
        allLabel={t("workspace.surveys.templates.all_channels")}
        options={channelOptions(t)}
        value={channel}
        onChange={(value) => setSelected([value, industry, role])}
      />
      <FilterRow
        allLabel={t("workspace.surveys.templates.all_industries")}
        options={industryOptions(t)}
        value={industry}
        onChange={(value) => setSelected([channel, value, role])}
      />
      <FilterRow
        allLabel={t("workspace.surveys.templates.all_roles")}
        options={roleOptions(t)}
        value={role}
        onChange={(value) => setSelected([channel, industry, value])}
      />
    </div>
  );
};

const TemplateTags = ({ template, selected }: { template: SurveyTemplate; selected: TemplateFilter }) => {
  const { t } = useTranslation();
  const role = roleOptions(t).find((option) => option.value === template.role)?.label;
  const industry = industryTag(template.industries, selected[1], t);
  const channel = channelTag(template.channels, t);
  const tag = "rounded-sm border border-slate-300 bg-slate-50 px-1.5 py-0.5 text-xs text-slate-500";
  return (
    <div className="flex flex-wrap gap-1.5">
      <div
        className={cn(
          "rounded-sm border px-1.5 py-0.5 text-xs",
          template.role ? roleStyling[template.role] : "border-slate-300 bg-slate-50 text-slate-500"
        )}>
        {role}
      </div>
      {industry && <div className={tag}>{industry}</div>}
      {channel && <div className={cn(tag, "flex-nowrap")}>{channel}</div>}
      {usesBranchingLogic(template) && (
        <span title={t("workspace.surveys.templates.uses_branching_logic")}>
          <SplitIcon
            className="size-5 rounded-sm border border-slate-300 bg-slate-50 p-0.5 text-slate-400"
            aria-label={t("workspace.surveys.templates.uses_branching_logic")}
          />
        </span>
      )}
    </div>
  );
};

/**
 * forma2's TemplateList: "start from scratch" and the catalog's templates, narrowed by the channel, industry and role filters or by a name search. Choosing a card selects it; its button then creates the survey and opens the editor.
 */
export const TemplateList = ({
  workspaceId,
  surveyType,
  defaultLanguage,
  search,
  aiCard,
}: {
  workspaceId: string;
  surveyType: "link" | "app";
  defaultLanguage: string;
  search: string;
  /** The create-with-AI card, shown after "start from scratch" where the gallery is a workspace's first survey. */
  aiCard?: ReactNode;
}) => {
  const { t, i18n } = useTranslation();
  const catalog = useQuery(surveyTemplatesQuery(workspaceId, i18n.language));
  const [activeId, setActiveId] = useState<string | null>(null);
  const [filter, setFilter] = useState<TemplateFilter>(noTemplateFilter);
  const { create, isPending } = useCreateSurveyFromTemplate();

  if (catalog.isPending) {
    return (
      <div className="px-6 py-4">
        <LoadingBlock />
      </div>
    );
  }
  if (catalog.isError) {
    return (
      <p role="alert" className="px-6 py-4 text-sm text-red-600">
        {errorMessage(t, catalog.error)}
      </p>
    );
  }

  const custom = catalog.data.templates.find((template) => template.id === CUSTOM_TEMPLATE_ID);
  const templates = filterTemplates(
    catalog.data.templates.filter((template) => template.id !== CUSTOM_TEMPLATE_ID),
    search,
    filter
  );
  const createFrom = (template: SurveyTemplate) =>
    void create({
      workspaceId,
      templateId: template.id,
      source: template.id === CUSTOM_TEMPLATE_ID ? "custom" : "catalog",
      surveyType,
      defaultLanguage,
    });

  return (
    <div className="relative z-0 flex-1 px-6 pt-2 pb-6">
      {!search && <TemplateFilters selected={filter} setSelected={setFilter} />}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        {custom && (
          <TemplateCard
            isActive={activeId === custom.id}
            onSelect={() => setActiveId(custom.id)}
            className={cn(
              "border-2 bg-transparent shadow-none",
              activeId === custom.id
                ? "border-transparent ring-2 ring-brand-dark"
                : "border-dashed border-slate-300 hover:border-brand-dark hover:ring-0"
            )}
            action={
              <Button
                className="mt-6 max-w-full px-6 py-3"
                loading={isPending}
                onClick={() => createFrom(custom)}>
                <span className="truncate">{t("common.create_survey")}</span>
              </Button>
            }>
            <PlusCircleIcon className="text-brand-dark size-8 transition-all duration-150 group-hover:scale-110" />
            <h3 className="text-md mt-3 mb-1 text-left font-bold text-slate-700">{custom.name}</h3>
            <p className="text-left text-xs text-slate-600">{custom.description}</p>
          </TemplateCard>
        )}
        {aiCard}
        {templates.map((template) => (
          <TemplateCard
            key={template.id}
            isActive={activeId === template.id}
            onSelect={() => setActiveId(template.id)}
            action={
              <Button
                className="mt-6 max-w-full px-6 py-3"
                loading={isPending}
                onClick={() => createFrom(template)}>
                <span className="truncate">{t("workspace.surveys.templates.use_this_template")}</span>
              </Button>
            }>
            <TemplateTags template={template} selected={filter} />
            <h3 className="text-md mt-3 mb-1 text-left font-bold text-slate-700">{template.name}</h3>
            <p className="text-left text-xs text-slate-600">{template.description}</p>
          </TemplateCard>
        ))}
      </div>
    </div>
  );
};

/** A gallery card: a button until chosen, then a panel with the action that creates the survey. */
const TemplateCard = ({
  isActive,
  onSelect,
  action,
  className,
  children,
}: {
  isActive: boolean;
  onSelect: () => void;
  action: ReactNode;
  className?: string;
  children: ReactNode;
}) => {
  const classes = cn(
    "group relative flex flex-col rounded-lg bg-white p-6 text-left shadow-sm transition-all duration-150 hover:ring-2 hover:ring-slate-300",
    isActive && "ring-2 ring-slate-400",
    className
  );
  if (!isActive) {
    return (
      <button type="button" className={cn(classes, "cursor-pointer")} onClick={onSelect}>
        {children}
      </button>
    );
  }
  return (
    <div className={classes}>
      {children}
      <div className="flex justify-start">{action}</div>
    </div>
  );
};
