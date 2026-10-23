import * as Collapsible from "@radix-ui/react-collapsible";
import { CheckIcon } from "lucide-react";
import { UseFormReturn } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { TSurveyStyling } from "@forma/types/surveys/types";
import { TWorkspaceStyling } from "@forma/types/workspace";
import { cn } from "../../../lib/cn";
import { SurveyBgSelectorTab } from "./survey-bg-selector-tab";
import { Badge } from "../badge";
import { ColorPicker } from "../color-picker/index";
import { FormControl, FormDescription, FormField, FormItem, FormLabel } from "../form";
import { Slider } from "../slider";

interface BackgroundStylingCardProps {
  open: boolean;
  setOpen: React.Dispatch<React.SetStateAction<boolean>>;
  colors: string[];
  isSettingsPage?: boolean;
  disabled?: boolean;
  form: UseFormReturn<TWorkspaceStyling | TSurveyStyling>;
}

export const BackgroundStylingCard = ({
  open,
  setOpen,
  colors,
  isSettingsPage = false,
  disabled,
  form,
}: BackgroundStylingCardProps) => {
  const { t } = useTranslation();

  return (
    <Collapsible.Root
      open={open}
      onOpenChange={(openState) => {
        if (disabled) return;
        setOpen(openState);
      }}
      className={cn(
        open ? "" : "hover:bg-slate-50",
        "w-full space-y-2 rounded-lg border border-slate-300 bg-white"
      )}>
      <Collapsible.CollapsibleTrigger
        asChild
        disabled={disabled}
        data-testid="background-styling-card-trigger"
        className={cn(
          "w-full cursor-pointer rounded-lg hover:bg-slate-50",
          disabled && "cursor-not-allowed opacity-60 hover:bg-white"
        )}>
        <div className="inline-flex px-4 py-4">
          {!isSettingsPage && (
            <div className="flex items-center pr-5 pl-2">
              <CheckIcon
                strokeWidth={3}
                className="size-7 rounded-full border border-green-300 bg-green-100 p-1.5 text-green-600"
              />
            </div>
          )}
          <div className="flex flex-col">
            <div className="flex items-center gap-2">
              <p className={cn("font-semibold text-slate-800", isSettingsPage ? "text-sm" : "text-base")}>
                {t("workspace.surveys.edit.background_styling")}
              </p>
              {isSettingsPage && <Badge type="gray" size="normal" text={t("common.link_surveys")} />}
            </div>
            <p className={cn("mt-1 text-slate-500", isSettingsPage ? "text-xs" : "text-sm")}>
              {t("workspace.surveys.edit.change_the_background_to_a_color_image_or_animation")}
            </p>
          </div>
        </div>
      </Collapsible.CollapsibleTrigger>
      <Collapsible.CollapsibleContent className="data-[state=closed]:animate-collapsible-up data-[state=open]:animate-collapsible-down flex flex-col overflow-hidden">
        <hr className="pt-1 text-slate-600" />
        <div className="flex flex-col gap-6 p-6 pt-2">
          <FormField
            control={form.control}
            name="background"
            render={({ field }) => (
              <FormItem>
                <div>
                  <FormLabel>{t("workspace.surveys.edit.change_background")}</FormLabel>
                  <FormDescription>
                    {t("workspace.surveys.edit.pick_a_background_from_our_library_or_upload_your_own")}
                  </FormDescription>
                </div>

                <FormControl>
                  <SurveyBgSelectorTab
                    bg={field.value?.bg ?? ""}
                    handleBgChange={(bg: string, bgType: string) => {
                      field.onChange({
                        ...field.value,
                        bg,
                        bgType,
                        brightness: 100,
                      });
                    }}
                    colors={colors}
                    bgType={field.value?.bgType ?? "color"}
                  />
                </FormControl>
              </FormItem>
            )}
          />

          <div className="flex flex-col justify-center">
            <div className="flex flex-col gap-4">
              <div className="flex flex-col justify-center">
                <FormField
                  control={form.control}
                  name="background.brightness"
                  render={({ field }) => (
                    <FormItem>
                      <div>
                        <FormLabel>{t("workspace.surveys.edit.brightness")}</FormLabel>
                        <FormDescription>
                          {t("workspace.surveys.edit.darken_or_lighten_background_of_your_choice")}
                        </FormDescription>
                      </div>

                      <FormControl>
                        <div className="rounded-lg border bg-slate-50 p-6">
                          <Slider
                            value={[field.value ?? 100]}
                            max={200}
                            onValueChange={(value) => {
                              field.onChange(value[0]);
                            }}
                          />
                        </div>
                      </FormControl>
                    </FormItem>
                  )}
                />
              </div>
            </div>
          </div>

          <FormField
            control={form.control}
            name="footerLinkColor"
            render={({ field }) => (
              <FormItem className="space-y-1">
                <FormLabel>{t("workspace.look.advanced_styling_field_link_color")}</FormLabel>
                <FormDescription>
                  {t("workspace.look.advanced_styling_field_link_color_description")}
                </FormDescription>
                <FormControl>
                  <ColorPicker
                    color={field.value?.light ?? ""}
                    onChange={(color) => {
                      // A cleared picker means "auto-adjust": store undefined instead of an
                      // empty string so the strict ZColor schema keeps validating.
                      field.onChange(color ? { light: color } : undefined);
                    }}
                    containerClass="w-full"
                    placeholder={t("workspace.look.advanced_styling_field_link_color_placeholder")}
                  />
                </FormControl>
              </FormItem>
            )}
          />
        </div>
      </Collapsible.CollapsibleContent>
    </Collapsible.Root>
  );
};
