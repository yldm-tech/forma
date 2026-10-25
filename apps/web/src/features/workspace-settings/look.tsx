import type { TAppSurveyCardArrangementOptions, TCardArrangementOptions } from "@forma/types/styling";
import type { TWorkspaceStyling } from "@forma/types/workspace";
import { useQuery } from "@tanstack/react-query";
import { ChevronDownIcon, RotateCcwIcon, SparklesIcon } from "lucide-react";
import { type ChangeEvent, type FormEvent, type ReactNode, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Alert, AlertDescription } from "../../components/ui/alert";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { ColorPicker } from "../../components/ui/color-picker";
import { ConfirmationModal } from "../../components/ui/confirmation-modal";
import { DeleteDialog } from "../../components/ui/delete-dialog";
import { FileInput } from "../../components/ui/file-input";
import { useIsStorageConfigured } from "../../components/ui/file-input/components/uploader";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { PageContentWrapper, PageHeader, SettingsCard } from "../../components/ui/page";
import { Select } from "../../components/ui/select";
import { showStorageNotConfiguredToast } from "../../components/ui/storage-not-configured-toast";
import { Switch } from "../../components/ui/switch";
import { cn } from "../../lib/cn";
import { errorMessage } from "../../lib/errors";
import { type Workspace, type WorkspaceDetail, useUpdateWorkspace } from "../../lib/queries/workspaces";
import {
  STYLE_DEFAULTS,
  SURVEY_BG_COLORS,
  applySuggestedColors,
  formatDimension,
  parseDimension,
  themeFormValues,
} from "../../lib/styling";
import { toast } from "../../lib/toast";
import { handleFileUpload, showFileUploadErrorToast } from "../../lib/upload";
import { instanceQuery } from "../../lib/queries/instance";
import { ThemePreview } from "./theme-preview";

const ReadOnlyNotice = () => {
  const { t } = useTranslation();
  return (
    <Alert variant="warning" className="mt-4" role="status">
      <AlertDescription>
        {t("common.only_owners_managers_and_manage_access_members_can_perform_this_action")}
      </AlertDescription>
    </Alert>
  );
};

/**
 * forma2's appearance page: the theme with its live preview, the logo, the app survey placement and the Forma branding switches. Without file storage a warning says that uploads will fail.
 */
export const WorkspaceLookPage = ({ workspace }: { workspace: WorkspaceDetail }) => {
  const { t } = useTranslation();
  const { isReadOnly } = workspace.access;
  const { data: instance } = useQuery(instanceQuery);
  return (
    <PageContentWrapper>
      <PageHeader pageTitle={t("common.appearance")} />
      {instance?.storageConfigured === false && (
        <Alert variant="warning" role="status">
          <AlertDescription>{t("common.storage_not_configured")}</AlertDescription>
        </Alert>
      )}
      <SettingsCard
        width="full"
        title={t("workspace.look.theme")}
        description={t("workspace.look.theme_settings_description")}>
        {isReadOnly ? (
          <div className="pb-4">
            <ReadOnlyNotice />
          </div>
        ) : (
          <ThemeStyling key={JSON.stringify(workspace.styling)} workspace={workspace} />
        )}
      </SettingsCard>
      <SettingsCard
        width="full"
        title={t("common.logo")}
        description={t("workspace.look.logo_settings_description")}>
        <EditLogo key={JSON.stringify(workspace.logo)} workspace={workspace} isReadOnly={isReadOnly} />
      </SettingsCard>
      <SettingsCard
        width="full"
        title={t("workspace.look.app_survey_placement")}
        description={t("workspace.look.app_survey_placement_settings_description")}>
        <EditPlacement
          key={`${workspace.placement}-${workspace.overlay}-${workspace.clickOutsideClose}`}
          workspace={workspace}
          isReadOnly={isReadOnly}
        />
      </SettingsCard>
      <SettingsCard
        width="full"
        title={t("workspace.look.forma_branding")}
        description={t("workspace.look.forma_branding_settings_description")}>
        <div className="space-y-4 pb-4">
          <EditBranding type="linkSurvey" workspace={workspace} isReadOnly={isReadOnly} />
          <EditBranding type="appSurvey" workspace={workspace} isReadOnly={isReadOnly} />
          {isReadOnly && <ReadOnlyNotice />}
        </div>
      </SettingsCard>
    </PageContentWrapper>
  );
};

type StylingSetter = (update: (styling: TWorkspaceStyling) => TWorkspaceStyling) => void;

type ColorKey = {
  [K in keyof TWorkspaceStyling]-?: NonNullable<TWorkspaceStyling[K]> extends { light: string } ? K : never;
}[keyof TWorkspaceStyling];

type ValueKey = {
  [K in keyof TWorkspaceStyling]-?: NonNullable<TWorkspaceStyling[K]> extends number | string ? K : never;
}[keyof TWorkspaceStyling];

/** One field of the theme form: a color, a dimension (pixels or another unit), a plain number or text. */
type FieldSpec =
  | { kind: "color"; name: ColorKey; label: string; description: string }
  | { kind: "dimension"; name: ValueKey; label: string; description: string }
  | {
      kind: "number";
      name: ValueKey;
      label: string;
      description: string;
      step: number;
      min?: number;
      max?: number;
    }
  | { kind: "text"; name: ValueKey; label: string; description: string };

const StylingField = ({
  spec,
  styling,
  setStyling,
}: {
  spec: FieldSpec;
  styling: TWorkspaceStyling;
  setStyling: StylingSetter;
}) => {
  const id = `styling-${spec.name}`;
  const set = (value: unknown) => setStyling((current) => ({ ...current, [spec.name]: value }));
  let control: ReactNode;
  if (spec.kind === "color") {
    const color = styling[spec.name] as { light: string; dark?: string } | null | undefined;
    control = (
      <ColorPicker
        aria-label={spec.label}
        color={color?.light ?? ""}
        onChange={(light) => setStyling((current) => ({ ...current, [spec.name]: { ...color, light } }))}
      />
    );
  } else if (spec.kind === "dimension") {
    const { amount, unit } = parseDimension(styling[spec.name] as number | string | null | undefined);
    control = (
      <div className="focus-within:border-ink flex h-10 rounded-md border border-slate-300 focus-within:ring-2 focus-within:ring-slate-400 focus-within:ring-offset-2">
        <Input
          id={id}
          type="number"
          value={amount ?? ""}
          className="flex-1 rounded-r-none border-0 shadow-none focus:ring-0 focus:ring-offset-0"
          onChange={(e) => {
            if (e.target.value === "") return set(null);
            const next = Number.parseFloat(e.target.value);
            if (!Number.isNaN(next)) set(formatDimension(next, unit));
          }}
        />
        <Select
          aria-label={`${spec.label} unit`}
          value={unit}
          className="h-full w-[70px] rounded-l-none border-0 border-l border-slate-300"
          onChange={(e) => set(formatDimension(amount ?? 0, e.target.value as "px" | "%" | "rem" | "em"))}>
          {(["px", "%", "rem", "em"] as const).map((u) => (
            <option key={u} value={u}>
              {u}
            </option>
          ))}
        </Select>
      </div>
    );
  } else if (spec.kind === "number") {
    control = (
      <Input
        id={id}
        type="number"
        step={spec.step}
        min={spec.min}
        max={spec.max}
        value={(styling[spec.name] as number | string | null | undefined) ?? ""}
        onChange={(e) => set(Number.isNaN(e.target.valueAsNumber) ? null : e.target.valueAsNumber)}
      />
    );
  } else {
    control = (
      <Input
        id={id}
        value={(styling[spec.name] as string | null | undefined) ?? ""}
        onChange={(e) => set(e.target.value)}
      />
    );
  }
  return (
    <div className="space-y-1">
      <Label htmlFor={spec.kind === "color" ? undefined : id}>{spec.label}</Label>
      <p className="text-xs text-slate-500">{spec.description}</p>
      {control}
    </div>
  );
};

/** A collapsible group of the theme form. */
const Collapsible = ({
  title,
  description,
  badge,
  children,
  nested = false,
}: {
  title: string;
  description?: string;
  badge?: ReactNode;
  children: ReactNode;
  nested?: boolean;
}) => {
  const [open, setOpen] = useState(false);
  return (
    <div
      className={cn("w-full rounded-lg border bg-white", nested ? "border-slate-200" : "border-slate-300")}>
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
        className="flex w-full cursor-pointer items-center justify-between rounded-lg px-4 py-4 text-left hover:bg-slate-50">
        <span>
          <span className="flex items-center gap-2 text-sm font-semibold text-slate-800">
            {title}
            {badge}
          </span>
          {description && <span className="mt-1 block text-xs text-slate-500">{description}</span>}
        </span>
        <ChevronDownIcon className={cn("size-4 text-slate-500 transition-transform", open && "rotate-180")} />
      </button>
      {open && <div className="border-t border-slate-200 p-6">{children}</div>}
    </div>
  );
};

const FieldGrid = ({
  fields,
  styling,
  setStyling,
}: {
  fields: FieldSpec[];
  styling: TWorkspaceStyling;
  setStyling: StylingSetter;
}) => (
  <div className="grid grid-cols-2 gap-4">
    {fields.map((spec) => (
      <StylingField key={spec.name} spec={spec} styling={styling} setStyling={setStyling} />
    ))}
  </div>
);

/** forma2's ThemeStyling: the workspace theme, edited beside a live preview; saving writes the whole styling, resetting writes STYLE_DEFAULTS. */
const ThemeStyling = ({ workspace }: { workspace: Workspace }) => {
  const { t } = useTranslation();
  const update = useUpdateWorkspace(workspace.id);
  const [styling, setStylingState] = useState(() => themeFormValues(workspace.styling));
  // The preview takes the brand color only when it is committed (suggest, save, reset), not on every keystroke in the picker.
  const [previewBrandColor, setPreviewBrandColor] = useState(
    styling.brandColor?.light ?? STYLE_DEFAULTS.brandColor?.light ?? ""
  );
  const [previewType, setPreviewType] = useState<"link" | "app">("link");
  const [confirmReset, setConfirmReset] = useState(false);
  const [confirmSuggest, setConfirmSuggest] = useState(false);
  const setStyling: StylingSetter = (change) => setStylingState((current) => change(current));
  const headline = (key: string) => t(`workspace.look.advanced_styling_field_${key}`);

  const save = async (next: TWorkspaceStyling, message: string) => {
    try {
      await update.mutateAsync({ styling: next });
      setPreviewBrandColor(next.brandColor?.light ?? STYLE_DEFAULTS.brandColor?.light ?? "");
      toast.success(message);
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  const submit = (e: FormEvent) => {
    e.preventDefault();
    void save(styling, t("workspace.look.styling_updated_successfully"));
  };

  const formFields: { title: string; fields: FieldSpec[] }[] = [
    {
      title: t("workspace.look.advanced_styling_section_headlines"),
      fields: [
        {
          kind: "color",
          name: "elementHeadlineColor",
          label: headline("headline_color"),
          description: headline("headline_color_description"),
        },
        {
          kind: "color",
          name: "elementDescriptionColor",
          label: headline("description_color"),
          description: headline("description_color_description"),
        },
        {
          kind: "dimension",
          name: "elementHeadlineFontSize",
          label: headline("headline_size"),
          description: headline("headline_size_description"),
        },
        {
          kind: "dimension",
          name: "elementDescriptionFontSize",
          label: headline("description_size"),
          description: headline("description_size_description"),
        },
        {
          kind: "number",
          name: "elementHeadlineFontWeight",
          label: headline("headline_weight"),
          description: headline("headline_weight_description"),
          step: 100,
          min: 100,
          max: 900,
        },
        {
          kind: "number",
          name: "elementDescriptionFontWeight",
          label: headline("description_weight"),
          description: headline("description_weight_description"),
          step: 100,
          min: 100,
          max: 900,
        },
        {
          kind: "color",
          name: "elementUpperLabelColor",
          label: headline("upper_label_color"),
          description: headline("upper_label_color_description"),
        },
        {
          kind: "dimension",
          name: "elementUpperLabelFontSize",
          label: headline("upper_label_size"),
          description: headline("upper_label_size_description"),
        },
        {
          kind: "number",
          name: "elementUpperLabelFontWeight",
          label: headline("upper_label_weight"),
          description: headline("upper_label_weight_description"),
          step: 100,
          min: 100,
          max: 900,
        },
      ],
    },
    {
      title: t("workspace.look.advanced_styling_section_inputs"),
      fields: [
        {
          kind: "color",
          name: "inputBgColor",
          label: t("workspace.surveys.edit.input_color"),
          description: t("workspace.surveys.edit.input_color_description"),
        },
        {
          kind: "color",
          name: "inputBorderColor",
          label: t("workspace.surveys.edit.input_border_color"),
          description: t("workspace.surveys.edit.input_border_color_description"),
        },
        {
          kind: "color",
          name: "inputTextColor",
          label: headline("input_text"),
          description: headline("input_text_description"),
        },
        {
          kind: "dimension",
          name: "inputBorderRadius",
          label: headline("border_radius"),
          description: headline("input_border_radius_description"),
        },
        {
          kind: "dimension",
          name: "inputHeight",
          label: headline("height"),
          description: headline("input_height_description"),
        },
        {
          kind: "dimension",
          name: "inputFontSize",
          label: headline("font_size"),
          description: headline("input_font_size_description"),
        },
        {
          kind: "dimension",
          name: "inputPaddingX",
          label: headline("padding_x"),
          description: headline("input_padding_x_description"),
        },
        {
          kind: "dimension",
          name: "inputPaddingY",
          label: headline("padding_y"),
          description: headline("input_padding_y_description"),
        },
        {
          kind: "number",
          name: "inputPlaceholderOpacity",
          label: headline("placeholder_opacity"),
          description: headline("input_placeholder_opacity_description"),
          step: 0.1,
          max: 1,
        },
        {
          kind: "text",
          name: "inputShadow",
          label: headline("shadow"),
          description: headline("input_shadow_description"),
        },
      ],
    },
    {
      title: t("workspace.look.advanced_styling_section_buttons"),
      fields: [
        {
          kind: "color",
          name: "buttonBgColor",
          label: headline("button_bg"),
          description: headline("button_bg_description"),
        },
        {
          kind: "color",
          name: "buttonTextColor",
          label: headline("button_text"),
          description: headline("button_text_description"),
        },
        {
          kind: "dimension",
          name: "buttonBorderRadius",
          label: headline("border_radius"),
          description: headline("button_border_radius_description"),
        },
        {
          kind: "dimension",
          name: "buttonHeight",
          label: headline("height"),
          description: headline("button_height_description"),
        },
        {
          kind: "dimension",
          name: "buttonFontSize",
          label: headline("font_size"),
          description: headline("button_font_size_description"),
        },
        {
          kind: "number",
          name: "buttonFontWeight",
          label: headline("font_weight"),
          description: headline("button_font_weight_description"),
          step: 100,
          min: 100,
          max: 900,
        },
        {
          kind: "dimension",
          name: "buttonPaddingX",
          label: headline("padding_x"),
          description: headline("button_padding_x_description"),
        },
        {
          kind: "dimension",
          name: "buttonPaddingY",
          label: headline("padding_y"),
          description: headline("button_padding_y_description"),
        },
      ],
    },
    {
      title: t("workspace.look.advanced_styling_section_options"),
      fields: [
        {
          kind: "color",
          name: "optionBgColor",
          label: headline("option_bg"),
          description: headline("option_bg_description"),
        },
        {
          kind: "color",
          name: "optionLabelColor",
          label: headline("option_label"),
          description: headline("option_label_description"),
        },
        {
          kind: "color",
          name: "optionBorderColor",
          label: headline("option_border"),
          description: headline("option_border_description"),
        },
        {
          kind: "dimension",
          name: "optionBorderRadius",
          label: headline("border_radius"),
          description: headline("option_border_radius_description"),
        },
        {
          kind: "dimension",
          name: "optionPaddingX",
          label: headline("padding_x"),
          description: headline("option_padding_x_description"),
        },
        {
          kind: "dimension",
          name: "optionPaddingY",
          label: headline("padding_y"),
          description: headline("option_padding_y_description"),
        },
        {
          kind: "dimension",
          name: "optionFontSize",
          label: headline("font_size"),
          description: headline("option_font_size_description"),
        },
      ],
    },
  ];

  const isApp = previewType === "app";
  const arrangementOptions: { value: TCardArrangementOptions; label: string }[] = [
    { value: "simple", label: t("workspace.surveys.edit.simple") },
    { value: "straight", label: t("workspace.surveys.edit.straight") },
    { value: "casual", label: t("workspace.surveys.edit.casual") },
    ...(isApp ? [] : [{ value: "cardless" as const, label: t("workspace.surveys.edit.cardless") }]),
  ];
  const activeArrangement = isApp
    ? (styling.cardArrangement?.appSurveys ?? "straight")
    : (styling.cardArrangement?.linkSurveys ?? "straight");
  const setArrangement = (value: TCardArrangementOptions) =>
    setStyling((current) => ({
      ...current,
      cardArrangement: {
        linkSurveys: current.cardArrangement?.linkSurveys ?? "straight",
        appSurveys: current.cardArrangement?.appSurveys ?? "straight",
        ...(isApp ? { appSurveys: value as TAppSurveyCardArrangementOptions } : { linkSurveys: value }),
      },
    }));
  const background = styling.background;

  return (
    <form onSubmit={submit} className="pb-4">
      <div className="flex">
        <div className="relative flex w-1/2 flex-col pr-6">
          <div className="flex flex-1 flex-col gap-4">
            <div className="flex items-center gap-2 rounded-lg bg-slate-50 p-4">
              <Switch
                id="allowStyleOverwrite"
                aria-labelledby="allowStyleOverwrite-label"
                checked={styling.allowStyleOverwrite}
                onCheckedChange={(checked) =>
                  setStyling((current) => ({ ...current, allowStyleOverwrite: checked }))
                }
              />
              <div>
                <p id="allowStyleOverwrite-label" className="text-sm font-medium text-slate-800">
                  {t("workspace.look.enable_custom_styling")}
                </p>
                <p className="text-xs text-slate-500">
                  {t("workspace.look.enable_custom_styling_description")}
                </p>
              </div>
            </div>
            <div className="flex flex-col gap-4 rounded-lg bg-slate-50 p-4">
              <div className="grid grid-cols-2 items-end gap-4">
                <div className="space-y-1">
                  <Label>{t("workspace.surveys.edit.brand_color")}</Label>
                  <p className="text-xs text-slate-500">
                    {t("workspace.surveys.edit.brand_color_description")}
                  </p>
                  <ColorPicker
                    aria-label={t("workspace.surveys.edit.brand_color")}
                    color={styling.brandColor?.light ?? STYLE_DEFAULTS.brandColor?.light ?? ""}
                    onChange={(light) =>
                      setStyling((current) => ({ ...current, brandColor: { ...current.brandColor, light } }))
                    }
                  />
                </div>
                <Button
                  type="button"
                  className="h-10 justify-center gap-1"
                  onClick={() => setConfirmSuggest(true)}>
                  <SparklesIcon className="mr-2 size-4" />
                  {t("workspace.look.suggest_colors")}
                </Button>
              </div>
              <Collapsible
                title={t("workspace.surveys.edit.survey_styling")}
                description={t(
                  "workspace.surveys.edit.style_the_question_texts_descriptions_and_input_fields"
                )}>
                <div className="flex flex-col gap-6">
                  {formFields.map((section) => (
                    <Collapsible key={section.title} title={section.title} nested>
                      <FieldGrid fields={section.fields} styling={styling} setStyling={setStyling} />
                    </Collapsible>
                  ))}
                </div>
              </Collapsible>
              <Collapsible
                title={t("workspace.surveys.edit.card_styling")}
                description={t("workspace.surveys.edit.style_the_survey_card")}>
                <div className="flex flex-col gap-6">
                  <FieldGrid
                    styling={styling}
                    setStyling={setStyling}
                    fields={[
                      {
                        kind: "dimension",
                        name: "roundness",
                        label: t("workspace.surveys.edit.roundness"),
                        description: t("workspace.surveys.edit.roundness_description"),
                      },
                      {
                        kind: "color",
                        name: "cardBackgroundColor",
                        label: t("workspace.surveys.edit.card_background_color"),
                        description: t("workspace.surveys.edit.card_background_color_description"),
                      },
                      {
                        kind: "color",
                        name: "cardBorderColor",
                        label: t("workspace.surveys.edit.card_border_color"),
                        description: t("workspace.surveys.edit.card_border_color_description"),
                      },
                    ]}
                  />
                  <OptionTabs
                    label={t("workspace.surveys.edit.card_arrangement_for_survey_type_derived", {
                      surveyTypeDerived: isApp ? "App" : "Link",
                    })}
                    options={arrangementOptions}
                    value={activeArrangement}
                    onChange={setArrangement}
                  />
                  {!isApp && (
                    <OptionTabs
                      label={t("workspace.surveys.edit.card_width_for_link_surveys")}
                      options={[
                        { value: "narrow", label: t("workspace.surveys.edit.narrow") },
                        { value: "default", label: t("workspace.surveys.edit.default") },
                        { value: "wide", label: t("workspace.surveys.edit.wide") },
                      ]}
                      value={styling.linkSurveyCardWidth ?? "default"}
                      onChange={(linkSurveyCardWidth) =>
                        setStyling((current) => ({ ...current, linkSurveyCardWidth }))
                      }
                    />
                  )}
                  <hr className="text-slate-600" />
                  <ToggleRow
                    id="highlightBorderColor"
                    title={t("workspace.surveys.edit.add_highlight_border")}
                    description={t("workspace.surveys.edit.add_highlight_border_description")}
                    checked={!!styling.highlightBorderColor}
                    onChange={(checked) =>
                      setStyling((current) => ({
                        ...current,
                        highlightBorderColor: checked
                          ? { light: STYLE_DEFAULTS.highlightBorderColor?.light ?? "" }
                          : null,
                      }))
                    }>
                    {styling.highlightBorderColor && (
                      <ColorPicker
                        aria-label={t("workspace.surveys.edit.add_highlight_border")}
                        containerClass="w-1/2"
                        color={styling.highlightBorderColor.light}
                        onChange={(light) =>
                          setStyling((current) => ({
                            ...current,
                            highlightBorderColor: { ...current.highlightBorderColor, light },
                          }))
                        }
                      />
                    )}
                  </ToggleRow>
                  <hr className="text-slate-600" />
                  <ToggleRow
                    id="hideProgressBar"
                    title={t("workspace.surveys.edit.hide_progress_bar")}
                    description={t("workspace.surveys.edit.disable_the_visibility_of_survey_progress")}
                    checked={!!styling.hideProgressBar}
                    onChange={(hideProgressBar) =>
                      setStyling((current) => ({ ...current, hideProgressBar }))
                    }>
                    {!styling.hideProgressBar && (
                      <FieldGrid
                        styling={styling}
                        setStyling={setStyling}
                        fields={[
                          {
                            kind: "color",
                            name: "progressTrackBgColor",
                            label: headline("track_bg"),
                            description: headline("track_bg_description"),
                          },
                          {
                            kind: "color",
                            name: "progressIndicatorBgColor",
                            label: headline("indicator_bg"),
                            description: headline("indicator_bg_description"),
                          },
                          {
                            kind: "dimension",
                            name: "progressTrackHeight",
                            label: headline("track_height"),
                            description: headline("track_height_description"),
                          },
                        ]}
                      />
                    )}
                  </ToggleRow>
                </div>
              </Collapsible>
              <Collapsible
                title={t("workspace.surveys.edit.background_styling")}
                description={t("workspace.surveys.edit.change_the_background_to_a_color_image_or_animation")}
                badge={<Badge type="gray" size="normal" text={t("common.link_surveys")} />}>
                <div className="flex flex-col gap-6">
                  <div className="space-y-2">
                    <Label>{t("workspace.surveys.edit.change_background")}</Label>
                    <p className="text-xs text-slate-500">
                      {t("workspace.surveys.edit.pick_a_background_from_our_library_or_upload_your_own")}
                    </p>
                    <ColorPicker
                      aria-label={t("workspace.surveys.edit.color")}
                      color={background?.bgType === "color" ? (background.bg ?? "") : ""}
                      onChange={(bg) =>
                        setStyling((current) => ({
                          ...current,
                          background: { ...current.background, bg, bgType: "color", brightness: 100 },
                        }))
                      }
                    />
                    <div className="flex flex-wrap gap-2">
                      {SURVEY_BG_COLORS.map((color) => (
                        <button
                          type="button"
                          key={color}
                          aria-label={color}
                          aria-pressed={background?.bg === color}
                          className={cn(
                            "size-8 cursor-pointer rounded-md border border-slate-300",
                            background?.bg === color && "ring-2 ring-slate-500 ring-offset-1"
                          )}
                          style={{ backgroundColor: color }}
                          onClick={() =>
                            setStyling((current) => ({
                              ...current,
                              background: {
                                ...current.background,
                                bg: color,
                                bgType: "color",
                                brightness: 100,
                              },
                            }))
                          }
                        />
                      ))}
                    </div>
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="background-brightness">{t("workspace.surveys.edit.brightness")}</Label>
                    <p className="text-xs text-slate-500">
                      {t("workspace.surveys.edit.darken_or_lighten_background_of_your_choice")}
                    </p>
                    <div className="rounded-lg border bg-slate-50 p-6">
                      <input
                        id="background-brightness"
                        type="range"
                        min={0}
                        max={200}
                        className="w-full"
                        value={background?.brightness ?? 100}
                        onChange={(e) =>
                          setStyling((current) => ({
                            ...current,
                            background: { ...current.background, brightness: e.target.valueAsNumber },
                          }))
                        }
                      />
                    </div>
                  </div>
                  <div className="space-y-1">
                    <Label>{headline("link_color")}</Label>
                    <p className="text-xs text-slate-500">{headline("link_color_description")}</p>
                    <ColorPicker
                      aria-label={headline("link_color")}
                      color={styling.footerLinkColor?.light ?? ""}
                      placeholder={headline("link_color_placeholder")}
                      onChange={(light) =>
                        // A cleared picker means "auto-adjust", stored as no color at all.
                        setStyling((current) => ({
                          ...current,
                          footerLinkColor: light ? { light } : undefined,
                        }))
                      }
                    />
                  </div>
                </div>
              </Collapsible>
            </div>
          </div>
          <div className="mt-4 flex items-center gap-2">
            <Button size="sm" type="submit" loading={update.isPending}>
              {t("common.save")}
            </Button>
            <Button
              type="button"
              size="sm"
              variant="ghost"
              className="flex items-center gap-2"
              onClick={() => setConfirmReset(true)}>
              {t("common.reset_to_default")}
              <RotateCcwIcon className="size-4" />
            </Button>
          </div>
        </div>
        <div className="relative w-1/2 rounded-lg bg-slate-100 pt-4">
          <div className="sticky top-4 mb-4">
            <ThemePreview
              workspace={workspace}
              styling={{ ...styling, brandColor: { light: previewBrandColor } }}
              previewType={previewType}
              setPreviewType={setPreviewType}
            />
          </div>
        </div>
      </div>
      <ConfirmationModal
        open={confirmSuggest}
        setOpen={setConfirmSuggest}
        title={t("workspace.look.generate_theme_header")}
        body={t("workspace.look.generate_theme_confirmation")}
        buttonText={t("workspace.look.generate_theme_btn")}
        buttonVariant="default"
        onConfirm={() => {
          const next = applySuggestedColors(styling);
          setStylingState(next);
          setPreviewBrandColor(next.brandColor?.light ?? "");
          toast.success(t("workspace.look.suggested_colors_applied_please_save"));
          setConfirmSuggest(false);
        }}
      />
      <ConfirmationModal
        open={confirmReset}
        setOpen={setConfirmReset}
        title={t("workspace.look.reset_styling")}
        body={t("workspace.look.reset_styling_confirmation")}
        buttonText={t("common.confirm")}
        buttonVariant="default"
        onConfirm={() => {
          setConfirmReset(false);
          setStylingState({ ...STYLE_DEFAULTS });
          void save({ ...STYLE_DEFAULTS }, t("workspace.look.styling_updated_successfully"));
        }}
      />
    </form>
  );
};

const OptionTabs = <V extends string>({
  label,
  options,
  value,
  onChange,
}: {
  label: string;
  options: { value: V; label: string }[];
  value: V;
  onChange: (value: V) => void;
}) => (
  <div className="space-y-2">
    <p className="text-sm font-medium text-slate-800">{label}</p>
    <div
      role="radiogroup"
      aria-label={label}
      className="flex w-full gap-2 rounded-md border border-slate-300 bg-white p-1">
      {options.map((option) => (
        <button
          type="button"
          role="radio"
          aria-checked={option.value === value}
          key={option.value}
          onClick={() => onChange(option.value)}
          className={cn(
            "flex-1 cursor-pointer rounded-md px-3 py-1.5 text-sm text-slate-700",
            option.value === value ? "bg-slate-200 font-medium" : "hover:bg-slate-100"
          )}>
          {option.label}
        </button>
      ))}
    </div>
  </div>
);

const ToggleRow = ({
  id,
  title,
  description,
  checked,
  onChange,
  children,
}: {
  id: string;
  title: string;
  description: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
  children?: ReactNode;
}) => (
  <div className="flex flex-col gap-4">
    <div className="flex items-center gap-2">
      <Switch id={id} aria-labelledby={`${id}-label`} checked={checked} onCheckedChange={onChange} />
      <div>
        <p id={`${id}-label`} className="text-sm text-slate-800">
          {title}
        </p>
        <p className="text-xs text-slate-500">{description}</p>
      </div>
    </div>
    {children}
  </div>
);

/** forma2's EditLogo: a logo is uploaded to the workspace's storage, or replaced, given a background color, saved, and removed. */
const EditLogo = ({ workspace, isReadOnly }: { workspace: Workspace; isReadOnly: boolean }) => {
  const { t } = useTranslation();
  const update = useUpdateWorkspace(workspace.id);
  const isStorageConfigured = useIsStorageConfigured();
  const [url, setUrl] = useState(workspace.logo?.url || undefined);
  const [isEditing, setIsEditing] = useState(false);
  const [isUploading, setIsUploading] = useState(false);
  const [bgColor, setBgColor] = useState(workspace.logo?.bgColor);
  const [confirmRemove, setConfirmRemove] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const replace = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!isStorageConfigured) {
      showStorageNotConfiguredToast(t);
      return;
    }
    if (file) {
      setIsUploading(true);
      const result = await handleFileUpload(file, workspace.id);
      setIsUploading(false);
      if (result.error) showFileUploadErrorToast(result.error, t);
      else setUrl(result.url);
    }
    setIsEditing(true);
  };

  const save = async () => {
    if (!isEditing) {
      setIsEditing(true);
      return;
    }
    try {
      await update.mutateAsync({ logo: { url, bgColor } });
      toast.success(t("workspace.look.logo_updated_successfully"));
    } catch {
      toast.error(t("workspace.look.failed_to_update_logo"));
    } finally {
      setIsEditing(false);
    }
  };

  const remove = async () => {
    setUrl(undefined);
    try {
      await update.mutateAsync({ logo: {} });
      toast.success(t("workspace.look.logo_removed_successfully"));
    } catch {
      toast.error(t("workspace.look.failed_to_remove_logo"));
    } finally {
      setIsEditing(false);
      setConfirmRemove(false);
    }
  };

  return (
    <div className="w-full space-y-6 pb-4" id="edit-logo">
      {url ? (
        <img
          src={url}
          alt={t("common.logo")}
          style={{ backgroundColor: bgColor || undefined }}
          className="h-20 w-auto max-w-64 rounded-lg border object-contain p-1"
        />
      ) : (
        <FileInput
          id="logo-input"
          workspaceId={workspace.id}
          onFileUpload={(files) => {
            if (files?.[0]) {
              setUrl(files[0]);
              setIsEditing(true);
            }
          }}
          disabled={isReadOnly}
          maxSizeInMB={5}
        />
      )}
      <input
        ref={fileInputRef}
        type="file"
        accept="image/jpeg, image/png, image/webp, image/heic"
        className="hidden"
        disabled={isReadOnly}
        data-testid="logo-replace-input"
        onChange={(e) => void replace(e)}
      />
      {isEditing && url && (
        <>
          <div className="flex gap-2">
            <Button
              variant="secondary"
              size="sm"
              loading={isUploading}
              onClick={() => {
                if (!isStorageConfigured) {
                  showStorageNotConfiguredToast(t);
                  return;
                }
                fileInputRef.current?.click();
              }}>
              {t("workspace.look.replace_logo")}
            </Button>
            <Button variant="destructive" size="sm" onClick={() => setConfirmRemove(true)}>
              {t("workspace.look.remove_logo")}
            </Button>
          </div>
          <ToggleRow
            id="addBackgroundColor"
            title={t("workspace.look.add_background_color")}
            description={t("workspace.look.add_background_color_description")}
            checked={!!bgColor}
            onChange={(enabled) => setBgColor(enabled ? (bgColor ?? "#f8f8f8") : undefined)}>
            {bgColor && (
              <ColorPicker
                aria-label={t("workspace.look.add_background_color")}
                color={bgColor}
                onChange={setBgColor}
              />
            )}
          </ToggleRow>
        </>
      )}
      {url && (
        <Button
          onClick={() => void save()}
          disabled={update.isPending || isUploading || isReadOnly}
          size="sm">
          {isEditing ? t("common.save") : t("common.edit")}
        </Button>
      )}
      <DeleteDialog
        open={confirmRemove}
        setOpen={setConfirmRemove}
        deleteWhat={t("common.logo")}
        text={t("workspace.look.remove_logo_confirmation")}
        onDelete={() => void remove()}
        isDeleting={update.isPending}
      />
      {isReadOnly && <ReadOnlyNotice />}
    </div>
  );
};

const OVERLAY_CLASSES: Record<Workspace["overlay"], string> = {
  none: "bg-slate-200",
  light: "bg-slate-400/50",
  dark: "bg-slate-700/80",
};

const PLACEMENT_CLASSES: Record<Workspace["placement"], string> = {
  bottomRight: "bottom-3 sm:right-3",
  topRight: "sm:top-6 sm:right-6",
  topLeft: "sm:top-6 sm:left-6",
  bottomLeft: "bottom-3 sm:left-3",
  center: "top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2",
};

/** forma2's EditPlacementForm: where app surveys appear, the overlay behind them and whether a click outside closes them. */
const EditPlacement = ({ workspace, isReadOnly }: { workspace: Workspace; isReadOnly: boolean }) => {
  const { t } = useTranslation();
  const update = useUpdateWorkspace(workspace.id);
  const [placement, setPlacement] = useState(workspace.placement);
  const [overlay, setOverlay] = useState(workspace.overlay);
  const [clickOutsideClose, setClickOutsideClose] = useState(workspace.clickOutsideClose);
  const placements: { value: Workspace["placement"]; label: string }[] = [
    { value: "bottomRight", label: t("common.bottom_right") },
    { value: "topRight", label: t("common.top_right") },
    { value: "topLeft", label: t("common.top_left") },
    { value: "bottomLeft", label: t("common.bottom_left") },
    { value: "center", label: t("common.centered_modal") },
  ];

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    try {
      await update.mutateAsync({ placement, overlay, clickOutsideClose });
      toast.success(t("workspace.look.placement_updated_successfully"));
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  return (
    <div className="pb-4">
      <form className="w-full items-center" onSubmit={submit}>
        <div className="flex">
          <div role="radiogroup" aria-label={t("workspace.look.app_survey_placement")} className="space-y-2">
            {placements.map((option) => (
              <div key={option.value} className="flex items-center gap-x-2 whitespace-nowrap">
                <input
                  type="radio"
                  id={`placement-${option.value}`}
                  name="placement"
                  value={option.value}
                  checked={placement === option.value}
                  disabled={isReadOnly}
                  onChange={() => setPlacement(option.value)}
                />
                <Label
                  htmlFor={`placement-${option.value}`}
                  className={isReadOnly ? "cursor-not-allowed opacity-50" : "cursor-pointer"}>
                  {option.label}
                </Label>
              </div>
            ))}
          </div>
          <div
            className={cn(
              "relative ml-8 h-40 w-full rounded-sm",
              overlay !== "none" && !clickOutsideClose && "cursor-not-allowed",
              OVERLAY_CLASSES[overlay]
            )}>
            <div className={cn("absolute h-16 w-16 rounded-sm bg-slate-700", PLACEMENT_CLASSES[placement])} />
          </div>
        </div>
        <div className="mt-6">
          <OptionTabs
            label={t("common.overlay_color")}
            options={[
              { value: "none" as const, label: t("common.no_overlay") },
              { value: "light" as const, label: t("common.light_overlay") },
              { value: "dark" as const, label: t("common.dark_overlay") },
            ]}
            value={overlay}
            onChange={setOverlay}
          />
        </div>
        {overlay !== "none" && (
          <div className="mt-6 space-y-2" role="radiogroup" aria-labelledby="clickOutsideClose-label">
            <p id="clickOutsideClose-label" className="text-sm font-semibold text-slate-800">
              {t("common.allow_users_to_exit_by_clicking_outside_the_survey")}
            </p>
            <div className="flex gap-x-4">
              {[
                { id: "disallow", value: false, label: t("common.disallow") },
                { id: "allow", value: true, label: t("common.allow") },
              ].map((option) => (
                <div key={option.id} className="flex items-center gap-x-2 whitespace-nowrap">
                  <input
                    type="radio"
                    id={option.id}
                    name="clickOutsideClose"
                    checked={clickOutsideClose === option.value}
                    disabled={isReadOnly}
                    onChange={() => setClickOutsideClose(option.value)}
                  />
                  <Label htmlFor={option.id}>{option.label}</Label>
                </div>
              ))}
            </div>
          </div>
        )}
        <Button
          className="mt-4 w-fit"
          size="sm"
          type="submit"
          loading={update.isPending}
          disabled={isReadOnly}>
          {t("common.save")}
        </Button>
      </form>
      {isReadOnly && <ReadOnlyNotice />}
    </div>
  );
};

/** forma2's EditBranding: shows or hides the Forma signature in link or app surveys. */
const EditBranding = ({
  type,
  workspace,
  isReadOnly,
}: {
  type: "linkSurvey" | "appSurvey";
  workspace: Workspace;
  isReadOnly: boolean;
}) => {
  const { t } = useTranslation();
  const update = useUpdateWorkspace(workspace.id);
  const stored = type === "linkSurvey" ? workspace.linkSurveyBranding : workspace.inAppSurveyBranding;
  const [enabled, setEnabled] = useState(stored);
  const id = `branding-${type}`;

  const toggle = async () => {
    const next = !enabled;
    setEnabled(next);
    try {
      await update.mutateAsync(
        type === "linkSurvey" ? { linkSurveyBranding: next } : { inAppSurveyBranding: next }
      );
      toast.success(
        next ? t("workspace.look.forma_branding_shown") : t("workspace.look.forma_branding_hidden")
      );
    } catch (error) {
      setEnabled(!next);
      toast.error(errorMessage(t, error));
    }
  };

  return (
    <div className="flex items-center gap-x-2">
      <Switch
        id={id}
        aria-labelledby={`${id}-label`}
        checked={enabled}
        onCheckedChange={() => void toggle()}
        disabled={update.isPending || isReadOnly}
      />
      <Label htmlFor={id} id={`${id}-label`}>
        {t("workspace.look.show_forma_branding_in", {
          type: type === "linkSurvey" ? t("common.link") : t("common.app"),
        })}
      </Label>
    </div>
  );
};
