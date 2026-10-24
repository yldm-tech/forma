import { InfoIcon, PlusIcon, Terminal, TrashIcon } from "lucide-react";
import { type ReactNode, useState } from "react";
import { useTranslation } from "react-i18next";
import { Alert, AlertDescription, AlertTitle } from "../../components/ui/alert";
import { Button } from "../../components/ui/button";
import { FormError } from "../../components/ui/form-error";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { Select } from "../../components/ui/select";
import { Switch } from "../../components/ui/switch";
import {
  type ActionFormValues,
  type NoCodeConfig,
  type NoCodeType,
  type UrlFilter,
  noCodeConfigForType,
  urlPassesFilters,
} from "../../lib/action-classes";
import { cn } from "../../lib/cn";
import { toast } from "../../lib/toast";

export const ACTION_DOCS_HREF = "https://forma.yldm.ai/docs/surveys/website-app-surveys/actions";

/** forma2's TabToggle: a row of mutually exclusive options. */
export const TabToggle = <V extends string>({
  label,
  options,
  value,
  onChange,
  disabled,
}: {
  label: string;
  options: { value: V; label: string }[];
  value: V;
  onChange: (value: V) => void;
  disabled?: boolean;
}) => (
  <div
    role="radiogroup"
    aria-label={label}
    className="flex w-full gap-1 rounded-md border border-slate-200 bg-slate-50 p-1">
    {options.map((option) => (
      <button
        type="button"
        role="radio"
        key={option.value}
        aria-checked={option.value === value}
        disabled={disabled}
        onClick={() => onChange(option.value)}
        className={cn(
          "flex-1 rounded-md px-2 py-1.5 text-sm whitespace-nowrap text-slate-700 disabled:cursor-not-allowed disabled:opacity-60",
          option.value === value ? "bg-white font-medium shadow-sm" : "hover:bg-slate-100"
        )}>
        {option.label}
      </button>
    ))}
  </div>
);

/** forma2's AdvancedOptionToggle: a switch with a title and description, and the option's own controls beneath while it is on. */
export const OptionToggle = ({
  id,
  title,
  description,
  checked,
  onToggle,
  disabled,
  children,
}: {
  id: string;
  title: string;
  description: string;
  checked: boolean;
  onToggle: (checked: boolean) => void;
  disabled?: boolean;
  children?: ReactNode;
}) => (
  <div className="py-2">
    <div className="flex items-center gap-x-3">
      <Switch
        id={id}
        aria-labelledby={`${id}-title`}
        checked={checked}
        onCheckedChange={onToggle}
        disabled={disabled}
      />
      <div>
        <p id={`${id}-title`} className="text-sm font-semibold text-slate-800">
          {title}
        </p>
        <p className="text-xs text-slate-500">{description}</p>
      </div>
    </div>
    {checked && children && <div className="mt-2 rounded-lg border border-slate-200 p-4">{children}</div>}
  </div>
);

/**
 * The fields of forma2's action forms: the name and description, then either the code action's key or the no-code action's trigger, element selectors, time on page and page filters. The action type itself is chosen by the create form only.
 */
export const ActionFields = ({
  values,
  onChange,
  errors,
  isReadOnly,
  isKeyReadOnly,
  idPrefix,
}: {
  values: ActionFormValues;
  onChange: (values: ActionFormValues) => void;
  errors: Record<string, string>;
  isReadOnly: boolean;
  /** A stored code action's key cannot change. */
  isKeyReadOnly: boolean;
  idPrefix: string;
}) => {
  const { t } = useTranslation();
  const set = (patch: Partial<ActionFormValues>) => onChange({ ...values, ...patch });
  return (
    <>
      <div className="grid w-full grid-cols-2 gap-x-4">
        <div className="space-y-1">
          <Label htmlFor={`${idPrefix}-name`}>{t("workspace.actions.what_did_your_user_do")}</Label>
          <Input
            id={`${idPrefix}-name`}
            value={values.name}
            onChange={(e) => set({ name: e.target.value })}
            placeholder={t("workspace.actions.eg_clicked_download")}
            isInvalid={!!errors.name}
            aria-describedby={errors.name ? `${idPrefix}-name-error` : undefined}
            disabled={isReadOnly}
          />
          <FormError id={`${idPrefix}-name-error`}>{errors.name}</FormError>
        </div>
        <div className="space-y-1">
          <Label htmlFor={`${idPrefix}-description`}>{t("common.description")}</Label>
          <Input
            id={`${idPrefix}-description`}
            value={values.description ?? ""}
            onChange={(e) => set({ description: e.target.value })}
            placeholder={t("workspace.actions.user_clicked_download_button")}
            disabled={isReadOnly}
          />
        </div>
      </div>
      <hr className="border-slate-200" />
      {values.type === "code" ? (
        <CodeActionFields
          values={values}
          set={set}
          errors={errors}
          isReadOnly={isKeyReadOnly}
          idPrefix={idPrefix}
        />
      ) : (
        <NoCodeActionFields
          config={values.noCodeConfig ?? noCodeConfigForType(null, "click")}
          onChange={(noCodeConfig) => set({ noCodeConfig })}
          errors={errors}
          isReadOnly={isReadOnly}
          idPrefix={idPrefix}
        />
      )}
    </>
  );
};

const CodeActionFields = ({
  values,
  set,
  errors,
  isReadOnly,
  idPrefix,
}: {
  values: ActionFormValues;
  set: (patch: Partial<ActionFormValues>) => void;
  errors: Record<string, string>;
  isReadOnly: boolean;
  idPrefix: string;
}) => {
  const { t } = useTranslation();
  return (
    <div className="space-y-4">
      <div className="space-y-1">
        <Label htmlFor={`${idPrefix}-key`}>{t("common.key")}</Label>
        <Input
          id={`${idPrefix}-key`}
          placeholder={t("workspace.actions.eg_download_cta_click_on_home")}
          className="mb-2 w-1/2"
          value={values.key ?? ""}
          onChange={(e) => set({ key: e.target.value })}
          isInvalid={!!errors.key}
          aria-describedby={errors.key ? `${idPrefix}-key-error` : undefined}
          readOnly={isReadOnly}
          disabled={isReadOnly}
        />
        <FormError id={`${idPrefix}-key-error`}>{errors.key}</FormError>
      </div>
      <Alert role="status">
        <Terminal className="size-4" />
        <AlertTitle>{t("workspace.actions.how_do_code_actions_work")}</AlertTitle>
        <AlertDescription>
          {t("workspace.actions.you_can_track_code_action_anywhere_in_your_app_using")}
          <span className="rounded-sm bg-slate-100 px-2 py-1 text-xs">
            forma.track(&quot;{values.key}&quot;)
          </span>{" "}
          {t("workspace.actions.in_your_code_read_more_in_our")}{" "}
          <a href={ACTION_DOCS_HREF} target="_blank" rel="noreferrer" className="underline">
            {t("common.docs")}
          </a>
          {"."}
        </AlertDescription>
      </Alert>
    </div>
  );
};

const NoCodeActionFields = ({
  config,
  onChange,
  errors,
  isReadOnly,
  idPrefix,
}: {
  config: NoCodeConfig;
  onChange: (config: NoCodeConfig) => void;
  errors: Record<string, string>;
  isReadOnly: boolean;
  idPrefix: string;
}) => {
  const { t } = useTranslation();
  const info = (title: string, description: string) => (
    <Alert>
      <InfoIcon className="size-4" />
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>{description}</AlertDescription>
    </Alert>
  );
  const selector = config.elementSelector ?? {};
  const setSelector = (patch: Partial<NonNullable<NoCodeConfig["elementSelector"]>>) =>
    onChange({ ...config, elementSelector: { ...selector, ...patch } });
  return (
    <div className="space-y-2">
      <div className="space-y-1">
        <Label>{t("workspace.actions.what_is_the_user_doing")}</Label>
        <TabToggle<NoCodeType>
          label={t("workspace.actions.what_is_the_user_doing")}
          disabled={isReadOnly}
          value={config.type}
          onChange={(type) => onChange(noCodeConfigForType(config, type))}
          options={[
            { value: "click", label: t("workspace.actions.click") },
            { value: "pageView", label: t("workspace.actions.page_view") },
            { value: "exitIntent", label: t("workspace.actions.exit_intent") },
            { value: "fiftyPercentScroll", label: t("workspace.actions.fifty_percent_scroll") },
            { value: "pageDwell", label: t("workspace.actions.time_on_page") },
          ]}
        />
      </div>
      <div className="mt-2">
        {config.type === "click" && (
          <div>
            <OptionToggle
              id={`${idPrefix}-css-selector`}
              title={t("workspace.actions.css_selector")}
              description={t("workspace.actions.if_a_user_clicks_a_button_with_a_specific_css_class_or_id")}
              checked={selector.cssSelector !== undefined}
              onToggle={(checked) => setSelector({ cssSelector: checked ? "" : undefined })}
              disabled={isReadOnly}>
              <Input
                aria-label={t("workspace.actions.css_selector")}
                placeholder={t("workspace.actions.add_css_class_or_id")}
                value={selector.cssSelector ?? ""}
                onChange={(e) => setSelector({ cssSelector: e.target.value })}
                isInvalid={!!errors["noCodeConfig.elementSelector.cssSelector"]}
                disabled={isReadOnly}
              />
              <FormError>{errors["noCodeConfig.elementSelector.cssSelector"]}</FormError>
            </OptionToggle>
            <OptionToggle
              id={`${idPrefix}-inner-text`}
              title={t("workspace.actions.inner_text")}
              description={t("workspace.actions.if_a_user_clicks_a_button_with_a_specific_text")}
              checked={selector.innerHtml !== undefined}
              onToggle={(checked) => setSelector({ innerHtml: checked ? "" : undefined })}
              disabled={isReadOnly}>
              <Input
                aria-label={t("workspace.actions.inner_text")}
                placeholder={t("workspace.actions.eg_install_app")}
                value={selector.innerHtml ?? ""}
                onChange={(e) => setSelector({ innerHtml: e.target.value })}
                disabled={isReadOnly}
              />
            </OptionToggle>
            <FormError>{errors["noCodeConfig.elementSelector"]}</FormError>
          </div>
        )}
        {config.type === "pageView" &&
          info(
            t("workspace.actions.page_view"),
            t("workspace.actions.this_action_will_be_triggered_when_the_page_is_loaded")
          )}
        {config.type === "exitIntent" &&
          info(
            t("workspace.actions.exit_intent"),
            t("workspace.actions.this_action_will_be_triggered_when_the_user_tries_to_leave_the_page")
          )}
        {config.type === "fiftyPercentScroll" &&
          info(
            t("workspace.actions.fifty_percent_scroll"),
            t("workspace.actions.this_action_will_be_triggered_when_the_user_scrolls_50_percent_of_the_page")
          )}
        {config.type === "pageDwell" && (
          <div className="space-y-2">
            {info(
              t("workspace.actions.time_on_page"),
              t("workspace.actions.this_action_will_be_triggered_after_user_stays_on_page")
            )}
            <Label htmlFor={`${idPrefix}-time`}>{t("workspace.actions.time_in_seconds")}</Label>
            <Input
              id={`${idPrefix}-time`}
              type="number"
              min={1}
              max={3600}
              className="w-1/2"
              placeholder={t("workspace.actions.time_in_seconds_placeholder")}
              value={config.timeInSeconds ?? ""}
              onChange={(e) =>
                onChange({
                  ...config,
                  timeInSeconds: e.target.value === "" ? undefined : Number(e.target.value),
                })
              }
              isInvalid={!!errors["noCodeConfig.timeInSeconds"]}
              disabled={isReadOnly}
            />
            <FormError>{errors["noCodeConfig.timeInSeconds"]}</FormError>
          </div>
        )}
      </div>
      <PageUrlFilters config={config} onChange={onChange} errors={errors} isReadOnly={isReadOnly} />
    </div>
  );
};

/** forma2's PageUrlSelector: every page, or only pages matching the filters, with a URL to test them against. */
const PageUrlFilters = ({
  config,
  onChange,
  errors,
  isReadOnly,
}: {
  config: NoCodeConfig;
  onChange: (config: NoCodeConfig) => void;
  errors: Record<string, string>;
  isReadOnly: boolean;
}) => {
  const { t } = useTranslation();
  const [testUrl, setTestUrl] = useState("");
  const [isMatch, setIsMatch] = useState<boolean | null>(null);
  const filters = config.urlFilters;
  const connector = config.urlFiltersConnector ?? "or";
  const rules: { value: UrlFilter["rule"]; label: string }[] = [
    { value: "exactMatch", label: t("workspace.actions.exactly_matches") },
    { value: "contains", label: t("workspace.actions.contains") },
    { value: "startsWith", label: t("workspace.actions.starts_with") },
    { value: "endsWith", label: t("workspace.actions.ends_with") },
    { value: "notMatch", label: t("workspace.actions.does_not_exactly_match") },
    { value: "notContains", label: t("workspace.actions.does_not_contain") },
    { value: "matchesRegex", label: t("workspace.actions.matches_regex") },
  ];
  const setFilters = (urlFilters: UrlFilter[]) => onChange({ ...config, urlFilters });

  const testMatch = () => {
    try {
      const match = urlPassesFilters(testUrl, config, t);
      setIsMatch(match);
      if (match) toast.success(t("workspace.actions.your_survey_would_be_shown_on_this_url"));
      else toast.error(t("workspace.actions.your_survey_would_not_be_shown"));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t("common.something_went_wrong_please_try_again"));
    }
  };

  return (
    <div className="mt-4 space-y-3">
      <div className="space-y-1">
        <Label className="font-semibold">{t("workspace.actions.page_filter")}</Label>
        <p className="text-sm font-normal text-slate-500">
          {t("workspace.actions.limit_the_pages_on_which_this_action_gets_captured")}
        </p>
        <TabToggle
          label={t("workspace.actions.page_filter")}
          disabled={isReadOnly}
          value={filters.length > 0 ? "specific" : "all"}
          onChange={(value) => setFilters(value === "all" ? [] : [{ rule: "exactMatch", value: "" }])}
          options={[
            { value: "all", label: t("workspace.actions.on_all_pages") },
            { value: "specific", label: t("workspace.actions.limit_to_specific_pages") },
          ]}
        />
      </div>
      {filters.length > 0 && (
        <div className="space-y-2 rounded-lg border border-slate-200 p-4">
          <Label>{t("workspace.actions.url")}</Label>
          {filters.map((filter, index) => (
            // oxlint-disable-next-line react/no-array-index-key -- filters are positional
            <div key={index} className="space-y-1">
              {index > 0 && (
                <Select
                  aria-label={t("workspace.actions.page_filter")}
                  className="w-24"
                  value={connector}
                  disabled={isReadOnly}
                  onChange={(e) =>
                    onChange({ ...config, urlFiltersConnector: e.target.value as "or" | "and" })
                  }>
                  <option value="or">{t("workspace.actions.or")}</option>
                  <option value="and">{t("workspace.actions.and")}</option>
                </Select>
              )}
              <div className="flex items-start gap-2">
                <Select
                  aria-label={t("workspace.actions.select_match_type")}
                  className="w-48"
                  value={filter.rule}
                  disabled={isReadOnly}
                  onChange={(e) =>
                    setFilters(
                      filters.map((f, i) =>
                        i === index ? { ...f, rule: e.target.value as UrlFilter["rule"] } : f
                      )
                    )
                  }>
                  {rules.map((rule) => (
                    <option key={rule.value} value={rule.value}>
                      {rule.label}
                    </option>
                  ))}
                </Select>
                <div className="flex-1">
                  <Input
                    aria-label={t("workspace.actions.url")}
                    placeholder={
                      filter.rule === "matchesRegex"
                        ? t("workspace.actions.add_regular_expression_here")
                        : t("workspace.actions.enter_url")
                    }
                    value={filter.value}
                    disabled={isReadOnly}
                    isInvalid={!!errors[`noCodeConfig.urlFilters.${index}.value`]}
                    onChange={(e) =>
                      setFilters(filters.map((f, i) => (i === index ? { ...f, value: e.target.value } : f)))
                    }
                  />
                  <FormError>{errors[`noCodeConfig.urlFilters.${index}.value`]}</FormError>
                </div>
                {filters.length > 1 && (
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    aria-label={t("common.remove")}
                    disabled={isReadOnly}
                    onClick={() => setFilters(filters.filter((_, i) => i !== index))}>
                    <TrashIcon className="size-4" />
                  </Button>
                )}
              </div>
            </div>
          ))}
          <Button
            type="button"
            variant="secondary"
            size="sm"
            disabled={isReadOnly}
            onClick={() => setFilters([...filters, { rule: "exactMatch", value: "" }])}>
            <PlusIcon className="mr-2 size-4" />
            {t("workspace.actions.add_url")}
          </Button>
          <div className="mt-4 space-y-1">
            <Label className="font-semibold">{t("workspace.actions.test_your_url")}</Label>
            <p className="text-sm font-normal text-slate-500">
              {t("workspace.actions.enter_a_url_to_see_if_a_user_visiting_it_would_be_tracked")}
            </p>
            <div className="flex items-center gap-2">
              <Input
                aria-label={t("workspace.actions.test_your_url")}
                placeholder="e.g. https://app.com/dashboard"
                value={testUrl}
                className={cn(
                  isMatch === null
                    ? "border-slate-200"
                    : isMatch
                      ? "border-green-500 bg-green-50"
                      : "border-red-200 bg-red-50"
                )}
                onChange={(e) => {
                  setTestUrl(e.target.value);
                  setIsMatch(null);
                }}
              />
              <Button type="button" variant="secondary" disabled={!testUrl} onClick={testMatch}>
                {t("workspace.actions.test_match")}
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
