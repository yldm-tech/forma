import { useWatch, type UseFormReturn } from "react-hook-form";
import { Terminal } from "lucide-react";
import type { TActionClassInput } from "@forma/types/action-classes";
import { useTranslation } from "react-i18next";
import { Alert, AlertDescription, AlertTitle } from "./alert";
import { FormControl, FormError, FormField, FormItem, FormLabel } from "./form";
import { Input } from "./input";

interface CodeActionFormProps {
  form: UseFormReturn<TActionClassInput>;
  isReadOnly: boolean;
}

export const CodeActionForm = ({ form, isReadOnly }: CodeActionFormProps) => {
  const { control } = form;
  const key = useWatch({ control, name: "key" });
  const { t } = useTranslation();
  return (
    <div data-testid="code-action-form" className="space-y-4">
      <div className="col-span-1">
        <FormField
          control={control}
          name="key"
          render={({ field, fieldState: { error } }) => (
            <FormItem>
              <FormLabel htmlFor="codeActionKeyInput">{t("common.key")}</FormLabel>

              <FormControl>
                <Input
                  id="codeActionKeyInput"
                  placeholder={t("workspace.actions.eg_download_cta_click_on_home")}
                  {...field}
                  className="mb-2 w-1/2"
                  value={field.value ?? ""}
                  isInvalid={!!error?.message}
                  readOnly={isReadOnly}
                  disabled={isReadOnly}
                />
              </FormControl>
              <FormError />
            </FormItem>
          )}
        />
      </div>
      <Alert role="status">
        <Terminal className="size-4" />
        <AlertTitle>{t("workspace.actions.how_do_code_actions_work")}</AlertTitle>
        <AlertDescription>
          {t("workspace.actions.you_can_track_code_action_anywhere_in_your_app_using")}
          <span className="rounded-sm bg-slate-100 px-2 py-1 text-xs">
            forma.track(&quot;{key}&quot;)
          </span>{" "}
          {t("workspace.actions.in_your_code")}
        </AlertDescription>
      </Alert>
    </div>
  );
};
