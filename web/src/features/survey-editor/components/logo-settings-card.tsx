import * as Collapsible from "@radix-ui/react-collapsible";
import { useParams } from "@tanstack/react-router";
import { CheckIcon } from "lucide-react";
import { type ChangeEvent, type Dispatch, type SetStateAction, useRef, useState } from "react";
import { UseFormReturn, useWatch } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { TSurveyStyling } from "@forma/types/surveys/types";
import { TWorkspaceStyling } from "@forma/types/workspace";
import { cn } from "../../../lib/cn";
import { AdvancedOptionToggle } from "../../../components/ui/advanced-option-toggle";
import { Button } from "../../../components/ui/button";
import { ColorPicker } from "../../../components/ui/color-picker/index";
import { useIsStorageConfigured } from "../../../components/ui/file-input/components/uploader";
import { FileInput } from "../../../components/ui/file-input/index";
import { FormControl, FormDescription, FormField, FormItem, FormLabel } from "../../../components/ui/form";
import { showStorageNotConfiguredToast } from "../../../components/ui/storage-not-configured-toast";
import { Switch } from "../../../components/ui/switch";
import { handleFileUpload, showFileUploadErrorToast } from "../../../lib/upload";

type LogoSettingsCardProps = {
  open: boolean;
  setOpen: Dispatch<SetStateAction<boolean>>;
  form: UseFormReturn<TWorkspaceStyling | TSurveyStyling>;
  disabled?: boolean;
};

export const LogoSettingsCard = ({ open, setOpen, form, disabled = false }: LogoSettingsCardProps) => {
  const { t } = useTranslation();
  const { workspaceId = "" }: { workspaceId?: string } = useParams({ strict: false });
  const isStorageConfigured = useIsStorageConfigured();
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [isLoading, setIsLoading] = useState(false);

  const logo = useWatch({ control: form.control, name: "logo" });
  const logoUrl = logo?.url;
  const logoBgColor = logo?.bgColor;
  const isBgColorEnabled = !!logoBgColor;
  const isLogoHidden = useWatch({ control: form.control, name: "isLogoHidden" });

  const setLogoUrl = (url: string | undefined) => {
    const currentLogo = form.getValues("logo");
    form.setValue("logo", url ? { ...currentLogo, url } : undefined);
  };

  const setLogoBgColor = (bgColor: string | undefined) => {
    const currentLogo = form.getValues("logo");
    form.setValue("logo", {
      ...currentLogo,
      url: logoUrl,
      bgColor,
    });
  };

  const handleFileInputChange = (files: string[] | undefined) => {
    const [file] = files ?? [];
    if (file) {
      setLogoUrl(file);
    }
  };

  // forma2's replace: a new file picked for the logo already shown is uploaded straight away.
  const handleHiddenFileChange = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (!isStorageConfigured) {
      showStorageNotConfiguredToast(t);
      return;
    }
    if (!file) return;
    setIsLoading(true);
    try {
      const uploadResult = await handleFileUpload(file, workspaceId);
      if (uploadResult.error) {
        showFileUploadErrorToast(uploadResult.error, t);
        return;
      }
      setLogoUrl(uploadResult.url);
    } finally {
      setIsLoading(false);
      // Reset the input so the same file can be selected again.
      if (fileInputRef.current) fileInputRef.current.value = "";
    }
  };

  const handleRemoveLogo = () => {
    form.setValue("logo", undefined);
  };

  const toggleBackgroundColor = (enabled: boolean) => {
    setLogoBgColor(enabled ? logoBgColor || "#f8f8f8" : undefined);
  };

  const handleBgColorChange = (color: string) => {
    setLogoBgColor(color);
  };

  return (
    <Collapsible.Root
      open={open}
      onOpenChange={(openState) => {
        if (disabled) return;
        setOpen(openState);
      }}
      className="w-full rounded-lg border border-slate-300 bg-white">
      <Collapsible.CollapsibleTrigger
        asChild
        disabled={disabled}
        className={cn(
          "w-full cursor-pointer rounded-lg hover:bg-slate-50",
          disabled && "cursor-not-allowed opacity-60 hover:bg-white"
        )}>
        <div className="inline-flex w-full px-4 py-4">
          <div className="flex items-center pr-5 pl-2">
            <CheckIcon
              strokeWidth={3}
              className="size-7 rounded-full border border-green-300 bg-green-100 p-1.5 text-green-600"
            />
          </div>

          <div>
            <p className="text-base font-semibold text-slate-800">
              {t("workspace.surveys.edit.logo_settings")}
            </p>
            <p className="mt-1 text-sm text-slate-500">{t("workspace.surveys.edit.customize_survey_logo")}</p>
          </div>
        </div>
      </Collapsible.CollapsibleTrigger>

      <Collapsible.CollapsibleContent className="data-[state=closed]:animate-collapsible-up data-[state=open]:animate-collapsible-down flex flex-col overflow-hidden">
        <hr className="py-1 text-slate-600" />

        <div className="flex flex-col gap-6 p-6 pt-2">
          <FormField
            control={form.control}
            name="isLogoHidden"
            render={({ field }) => (
              <FormItem className="flex items-center gap-2 gap-y-0">
                <FormControl>
                  <Switch checked={!!field.value} onCheckedChange={field.onChange} disabled={disabled} />
                </FormControl>
                <div>
                  <FormLabel className="text-base font-semibold text-slate-900">
                    {t("workspace.surveys.edit.hide_logo")}
                  </FormLabel>
                  <FormDescription className="text-sm text-slate-800">
                    {t("workspace.surveys.edit.hide_logo_from_survey")}
                  </FormDescription>
                </div>
              </FormItem>
            )}
          />

          {!isLogoHidden && (
            <div className="space-y-4">
              <div className="font-medium text-slate-800">
                {t("workspace.surveys.edit.overwrite_survey_logo")}
              </div>

              <input
                ref={fileInputRef}
                type="file"
                accept="image/jpeg, image/png, image/webp, image/heic"
                className="hidden"
                disabled={disabled}
                onChange={(e) => void handleHiddenFileChange(e)}
                data-testid="survey-logo-replace-input"
              />

              {logoUrl ? (
                <>
                  <div className="flex items-center gap-4">
                    <img
                      src={logoUrl}
                      alt="Survey Logo"
                      width={256}
                      height={56}
                      style={{ backgroundColor: logoBgColor || undefined }}
                      className="h-20 w-auto max-w-64 rounded-lg border object-contain p-1"
                    />
                  </div>

                  <div className="flex gap-2">
                    <Button
                      type="button"
                      onClick={() => {
                        if (!isStorageConfigured) {
                          showStorageNotConfiguredToast(t);
                          return;
                        }
                        fileInputRef.current?.click();
                      }}
                      variant="secondary"
                      size="sm"
                      disabled={disabled || isLoading}>
                      {t("workspace.look.replace_logo")}
                    </Button>
                    <Button
                      type="button"
                      variant="destructive"
                      size="sm"
                      onClick={handleRemoveLogo}
                      disabled={disabled}>
                      {t("workspace.look.remove_logo")}
                    </Button>
                  </div>

                  <AdvancedOptionToggle
                    isChecked={isBgColorEnabled}
                    onToggle={toggleBackgroundColor}
                    htmlId="surveyLogoBgColor"
                    title={t("workspace.look.add_background_color")}
                    description={t("workspace.look.add_background_color_description")}
                    childBorder
                    customContainerClass="p-0"
                    childrenContainerClass="overflow-visible"
                    disabled={disabled}>
                    {isBgColorEnabled && (
                      <div className="w-full p-2">
                        <ColorPicker
                          color={logoBgColor || "#f8f8f8"}
                          onChange={handleBgColorChange}
                          disabled={disabled}
                        />
                      </div>
                    )}
                  </AdvancedOptionToggle>
                </>
              ) : (
                <FileInput
                  id="survey-logo-input"
                  onFileUpload={handleFileInputChange}
                  disabled={disabled}
                  workspaceId={workspaceId}
                  maxSizeInMB={5}
                />
              )}
            </div>
          )}
        </div>
      </Collapsible.CollapsibleContent>
    </Collapsible.Root>
  );
};
