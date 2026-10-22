import { InfoIcon } from "lucide-react";
import { UseFormReturn, useWatch } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { TActionClassInput } from "@forma/types/action-classes";
import { Alert, AlertDescription, AlertTitle } from "../alert";
import { FormControl, FormError, FormField, FormItem } from "../form";
import { Input } from "../input";
import { Label } from "../label";
import { TabToggle } from "../tab-toggle";
import { CssSelector } from "./components/css-selector";
import { InnerHtmlSelector } from "./components/inner-html-selector";
import { PageUrlSelector } from "./components/page-url-selector";

interface NoCodeActionFormProps {
  form: UseFormReturn<TActionClassInput>;
  isReadOnly: boolean;
}

export const NoCodeActionForm = ({ form, isReadOnly }: NoCodeActionFormProps) => {
  const { control } = form;
  const noCodeType = useWatch({ control, name: "noCodeConfig.type" });
  const { t } = useTranslation();
  return (
    <div data-testid="no-code-action-form">
      <FormField
        name={`noCodeConfig.type`}
        control={control}
        render={({ field }) => (
          <FormItem>
            <FormControl>
              <div>
                <Label>{t("workspace.actions.what_is_the_user_doing")}</Label>
                <TabToggle
                  disabled={isReadOnly}
                  id="userAction"
                  {...field}
                  defaultSelected={field.value}
                  options={[
                    { value: "click", label: t("workspace.actions.click") },
                    { value: "pageView", label: t("workspace.actions.page_view") },
                    { value: "exitIntent", label: t("workspace.actions.exit_intent") },
                    { value: "fiftyPercentScroll", label: t("workspace.actions.fifty_percent_scroll") },
                    { value: "pageDwell", label: t("workspace.actions.time_on_page") },
                  ]}
                />
              </div>
            </FormControl>
          </FormItem>
        )}
      />

      <div className="mt-2">
        {noCodeType === "click" && (
          <FormField
            control={control}
            name="noCodeConfig.elementSelector"
            render={() => (
              <FormItem>
                <FormControl>
                  <div>
                    <CssSelector form={form} disabled={isReadOnly} />
                    <InnerHtmlSelector form={form} disabled={isReadOnly} />
                  </div>
                </FormControl>
                <FormError />
              </FormItem>
            )}
          />
        )}
        {noCodeType === "pageView" && (
          <Alert>
            <InfoIcon className="size-4" />
            <AlertTitle>{t("workspace.actions.page_view")}</AlertTitle>
            <AlertDescription>
              {t("workspace.actions.this_action_will_be_triggered_when_the_page_is_loaded")}
            </AlertDescription>
          </Alert>
        )}
        {noCodeType === "exitIntent" && (
          <Alert>
            <InfoIcon className="size-4" />
            <AlertTitle>{t("workspace.actions.exit_intent")}</AlertTitle>
            <AlertDescription>
              {t("workspace.actions.this_action_will_be_triggered_when_the_user_tries_to_leave_the_page")}
            </AlertDescription>
          </Alert>
        )}
        {noCodeType === "fiftyPercentScroll" && (
          <Alert>
            <InfoIcon className="size-4" />
            <AlertTitle>{t("workspace.actions.fifty_percent_scroll")}</AlertTitle>
            <AlertDescription>
              {t(
                "workspace.actions.this_action_will_be_triggered_when_the_user_scrolls_50_percent_of_the_page"
              )}
            </AlertDescription>
          </Alert>
        )}
        {noCodeType === "pageDwell" && (
          <div className="flex flex-col gap-2">
            <Alert>
              <InfoIcon className="size-4" />
              <AlertTitle>{t("workspace.actions.time_on_page")}</AlertTitle>
              <AlertDescription>
                {t("workspace.actions.this_action_will_be_triggered_after_user_stays_on_page")}
              </AlertDescription>
            </Alert>
            <FormField
              control={control}
              name="noCodeConfig.timeInSeconds"
              render={({ field }) => (
                <FormItem>
                  <Label>{t("workspace.actions.time_in_seconds")}</Label>
                  <FormControl>
                    <Input
                      type="number"
                      min={1}
                      max={3600}
                      placeholder={t("workspace.actions.time_in_seconds_placeholder")}
                      disabled={isReadOnly}
                      {...field}
                      value={field.value ?? ""}
                      onChange={(e) => {
                        const val = e.target.value;
                        field.onChange(val === "" ? undefined : Number(val));
                      }}
                    />
                  </FormControl>
                  <FormError />
                </FormItem>
              )}
            />
          </div>
        )}
        <PageUrlSelector form={form} isReadOnly={isReadOnly} />
      </div>
    </div>
  );
};
