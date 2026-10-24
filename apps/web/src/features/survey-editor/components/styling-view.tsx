import { RotateCcwIcon } from "lucide-react";
import React, { useEffect, useMemo, useState } from "react";
import { UseFormReturn, useForm, useWatch } from "react-hook-form";
import { toast } from "../../../lib/toast";
import { TransLink } from "../../../components/ui/trans-link";
import { Trans, useTranslation } from "react-i18next";
import type { TWorkspace as Workspace } from "@forma/types/workspace";
import { TSurvey, TSurveyStyling } from "@forma/types/surveys/types";
import { TWorkspaceStyling } from "@forma/types/workspace";
import { COLOR_DEFAULTS, STYLE_DEFAULTS, getSuggestedColors } from "../../../lib/survey/styling";
import { FormStylingSettings } from "./form-styling-settings";
import { LogoSettingsCard } from "./logo-settings-card";
import { AlertDialog } from "../../../components/ui/alert-dialog";
import { BackgroundStylingCard } from "../../../components/ui/background-styling-card/index";
import { Button } from "../../../components/ui/button";
import { CardStylingSettings } from "../../../components/ui/card-styling-settings";
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormProvider,
} from "../../../components/ui/form";
import { Switch } from "../../../components/ui/switch";
import { appHref } from "../../../lib/base-path";

interface StylingViewProps {
  workspace: Workspace;
  localSurvey: TSurvey;
  setLocalSurvey: React.Dispatch<React.SetStateAction<TSurvey>>;
  colors: string[];
  styling: TSurveyStyling | null;
  setStyling: React.Dispatch<React.SetStateAction<TSurveyStyling | null>>;
  localStylingChanges: TSurveyStyling | null;
  setLocalStylingChanges: React.Dispatch<React.SetStateAction<TSurveyStyling | null>>;
  isCxMode: boolean;
}

export const StylingView = ({
  colors,
  workspace,
  localSurvey,
  setLocalSurvey,
  setStyling,
  styling,
  localStylingChanges,
  setLocalStylingChanges,
  isCxMode,
}: StylingViewProps) => {
  const workspaceBasePath = `/workspaces/${workspace.id}`;
  const { t } = useTranslation();

  const savedWorkspaceStyling = workspace.styling as Partial<TWorkspaceStyling> | null;

  // Strip null/undefined values so they don't override STYLE_DEFAULTS.
  const cleanWorkspace = savedWorkspaceStyling
    ? Object.fromEntries(Object.entries(savedWorkspaceStyling).filter(([, v]) => v != null))
    : {};
  const cleanSurvey = localSurvey.styling
    ? Object.fromEntries(Object.entries(localSurvey.styling).filter(([, v]) => v != null))
    : {};

  const form = useForm<TSurveyStyling>({
    defaultValues: {
      ...STYLE_DEFAULTS,
      ...cleanWorkspace,
      ...cleanSurvey,
    },
  });

  const overwriteThemeStyling = useWatch({ control: form.control, name: "overwriteThemeStyling" });
  const setOverwriteThemeStyling = (value: boolean) => form.setValue("overwriteThemeStyling", value);

  const [formStylingOpen, setFormStylingOpen] = useState(false);
  const [logoSettingsOpen, setLogoSettingsOpen] = useState(false);
  const [cardStylingOpen, setCardStylingOpen] = useState(false);
  const [stylingOpen, setStylingOpen] = useState(false);
  const [confirmResetStylingModalOpen, setConfirmResetStylingModalOpen] = useState(false);
  const [confirmSuggestColorsOpen, setConfirmSuggestColorsOpen] = useState(false);

  const handleSuggestColors = () => {
    const currentBrandColor =
      form.getValues().brandColor?.light ?? STYLE_DEFAULTS.brandColor?.light ?? COLOR_DEFAULTS.brandColor;
    const suggested = getSuggestedColors(currentBrandColor);

    for (const [key, value] of Object.entries(suggested)) {
      form.setValue(key as keyof TSurveyStyling, value, { shouldDirty: true });
    }

    // Footer link color auto-adjusts for contrast when unset; clear any override so it
    // follows the freshly suggested palette instead of a stale custom value.
    form.setValue("footerLinkColor", undefined, { shouldDirty: true });

    toast.success(t("workspace.look.suggested_colors_applied_please_save"));
    setConfirmSuggestColorsOpen(false);
  };

  const onResetThemeStyling = () => {
    const { allowStyleOverwrite: _allowStyleOverwrite, ...baseStyling } = workspace.styling ?? {};

    setStyling({
      ...baseStyling,
      overwriteThemeStyling: true,
    });

    form.reset({
      ...baseStyling,
      overwriteThemeStyling: true,
    });

    setConfirmResetStylingModalOpen(false);
    toast.success(t("workspace.surveys.edit.styling_set_to_theme_styles"));
  };

  useEffect(() => {
    if (!overwriteThemeStyling) {
      setFormStylingOpen(false);
      setLogoSettingsOpen(false);
      setCardStylingOpen(false);
      setStylingOpen(false);
    }
  }, [overwriteThemeStyling]);

  useEffect(() => {
    // Every change of the form is written into the survey, so the preview and the save see it.
    return form.subscribe({
      formState: { values: true },
      callback: ({ values }) => {
        setLocalSurvey((prev) => ({
          ...prev,
          styling: {
            ...prev.styling,
            ...values,
          },
        }));
      },
    });
  }, [form, setLocalSurvey]);

  const defaultWorkspaceStyling = useMemo(() => {
    const { styling: workspaceStyling } = workspace;
    const { allowStyleOverwrite: _allowStyleOverwrite, ...baseStyling } = workspaceStyling ?? {};

    return baseStyling;
  }, [workspace]);

  const handleOverwriteToggle = (value: boolean) => {
    // survey styling from the server is surveyStyling, it could either be set or not
    // if its set and the toggle is turned off, we set the local styling to the server styling

    setOverwriteThemeStyling(value);

    // if the toggle is turned on, we set the local styling to the workspace styling
    if (value) {
      if (!styling) {
        // copy the workspace styling to the survey styling
        setStyling({
          ...defaultWorkspaceStyling,
          overwriteThemeStyling: true,
        });
        return;
      }

      // if there are local styling changes, we set the styling to the local styling changes that were previously stored
      if (localStylingChanges) {
        setStyling(localStylingChanges);
      }
      // if there are no local styling changes, we set the styling to the workspace styling
      else {
        setStyling({
          ...defaultWorkspaceStyling,
          overwriteThemeStyling: true,
        });
      }
    }

    // if the toggle is turned off, we store the local styling changes and set the styling to the workspace styling
    else {
      // copy the styling to localStylingChanges
      setLocalStylingChanges(styling);

      // copy the workspace styling to the survey styling
      setStyling({
        ...defaultWorkspaceStyling,
        overwriteThemeStyling: false,
      });
    }
  };

  return (
    <FormProvider {...form}>
      <form onSubmit={(e) => e.preventDefault()}>
        <div className="mt-12 space-y-3 p-5">
          <div className="flex items-center gap-4 rounded-lg border border-slate-300 bg-white p-4">
            <FormField
              control={form.control}
              name="overwriteThemeStyling"
              render={({ field }) => (
                <FormItem className="flex items-center gap-4 gap-y-0">
                  <FormControl>
                    <Switch
                      id="overwrite-theme-styling"
                      checked={!!field.value}
                      onCheckedChange={handleOverwriteToggle}
                    />
                  </FormControl>

                  <div>
                    <FormLabel
                      htmlFor="overwrite-theme-styling"
                      className="text-base font-semibold text-slate-900">
                      {t("workspace.surveys.edit.add_custom_styles")}
                    </FormLabel>
                    <FormDescription className="text-sm text-slate-500">
                      {t("workspace.surveys.edit.override_theme_with_individual_styles_for_this_survey")}
                    </FormDescription>
                  </div>
                </FormItem>
              )}
            />
          </div>

          <FormStylingSettings
            open={formStylingOpen}
            setOpen={setFormStylingOpen}
            disabled={!overwriteThemeStyling}
            form={form as UseFormReturn<TWorkspaceStyling | TSurveyStyling>}
            onSuggestColorsClick={() => setConfirmSuggestColorsOpen(true)}
          />

          <CardStylingSettings
            open={cardStylingOpen}
            setOpen={setCardStylingOpen}
            surveyType={localSurvey.type}
            disabled={!overwriteThemeStyling}
            form={form as UseFormReturn<TWorkspaceStyling | TSurveyStyling>}
          />

          {localSurvey.type === "link" && (
            <>
              <BackgroundStylingCard
                open={stylingOpen}
                setOpen={setStylingOpen}
                colors={colors}
                disabled={!overwriteThemeStyling}
                form={form as UseFormReturn<TWorkspaceStyling | TSurveyStyling>}
              />

              <LogoSettingsCard
                open={logoSettingsOpen}
                setOpen={setLogoSettingsOpen}
                disabled={!overwriteThemeStyling}
                form={form as UseFormReturn<TWorkspaceStyling | TSurveyStyling>}
              />
            </>
          )}

          {!isCxMode && (
            <div className="mt-4 flex h-8 items-center justify-between">
              <div>
                {overwriteThemeStyling && (
                  <Button
                    type="button"
                    variant="ghost"
                    className="flex items-center gap-2"
                    onClick={() => setConfirmResetStylingModalOpen(true)}>
                    {t("workspace.surveys.edit.reset_to_theme_styles")}
                    <RotateCcwIcon className="size-4" />
                  </Button>
                )}
              </div>
              <p className="text-sm text-slate-500">
                <Trans
                  i18nKey="workspace.surveys.edit.adjust_theme_in_look_and_feel_settings"
                  components={{
                    lookFeelLink: (
                      <TransLink
                        href={appHref(`${workspaceBasePath}/settings/workspace/look`)}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="font-semibold underline"
                      />
                    ),
                  }}
                />
              </p>
            </div>
          )}
          <AlertDialog
            open={confirmResetStylingModalOpen}
            setOpen={setConfirmResetStylingModalOpen}
            headerText={t("workspace.surveys.edit.reset_to_theme_styles")}
            mainText={t("workspace.surveys.edit.reset_to_theme_styles_main_text")}
            confirmBtnLabel={t("common.confirm")}
            onDecline={() => setConfirmResetStylingModalOpen(false)}
            onConfirm={onResetThemeStyling}
          />

          <AlertDialog
            open={confirmSuggestColorsOpen}
            setOpen={setConfirmSuggestColorsOpen}
            headerText={t("workspace.look.generate_theme_header")}
            mainText={t("workspace.look.generate_theme_confirmation")}
            confirmBtnLabel={t("workspace.look.generate_theme_btn")}
            declineBtnLabel={t("common.cancel")}
            onConfirm={handleSuggestColors}
            onDecline={() => setConfirmSuggestColorsOpen(false)}
          />
        </div>
      </form>
    </FormProvider>
  );
};
