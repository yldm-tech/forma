import { useMemo } from "react";
import { FormProvider, useForm, useWatch } from "react-hook-form";
import { toast } from "../../../lib/toast";
import { useTranslation } from "react-i18next";
import type { TActionClass as ActionClass } from "@forma/types/action-classes";
import { TActionClassInput } from "@forma/types/action-classes";
import { TSurvey } from "@forma/types/surveys/types";
import { errorMessage } from "../../../lib/errors";
import { ActionNameDescriptionFields } from "../../../components/ui/action-name-description-fields";
import { Button } from "../../../components/ui/button";
import { CodeActionForm } from "../../../components/ui/code-action-form";
import { FormField } from "../../../components/ui/form";
import { Label } from "../../../components/ui/label";
import { NoCodeActionForm } from "../../../components/ui/no-code-action-form/index";
import { TabToggle } from "../../../components/ui/tab-toggle";
import { useCreateActionClass } from "../../action-classes/api";
import { buildActionObject } from "../lib/action-builder";
import { createActionClassZodResolver, useActionClassKeys, validatePermissions } from "../lib/action-utils";

interface CreateNewActionTabProps {
  actionClasses: ActionClass[];
  isReadOnly: boolean;
  setLocalSurvey?: React.Dispatch<React.SetStateAction<TSurvey>>;
  setOpen: React.Dispatch<React.SetStateAction<boolean>>;
  workspaceId: string;
}

export const CreateNewActionTab = ({
  actionClasses,
  setOpen,
  isReadOnly,
  setLocalSurvey,
  workspaceId,
}: CreateNewActionTabProps) => {
  const { t } = useTranslation();
  const createActionClass = useCreateActionClass(workspaceId);
  const actionClassNames = useMemo(
    () => actionClasses.map((actionClass) => actionClass.name),
    [actionClasses]
  );

  const actionClassKeys = useActionClassKeys(actionClasses);

  const form = useForm<TActionClassInput>({
    defaultValues: {
      name: "",
      description: "",
      workspaceId,
      type: "noCode",
      noCodeConfig: {
        type: "click",
        elementSelector: {
          cssSelector: undefined,
          innerHtml: undefined,
        },
        urlFilters: [],
      },
    },
    resolver: createActionClassZodResolver(actionClassNames, actionClassKeys, t),
    mode: "onChange",
  });

  const { control, handleSubmit, reset } = form;
  const actionType = useWatch({ control, name: "type" });
  const { isSubmitting } = form.formState;

  const submitHandler = async (data: TActionClassInput) => {
    let updatedAction: TActionClassInput;
    try {
      validatePermissions(isReadOnly, t);
      updatedAction = buildActionObject(data, workspaceId, t);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e));
      return;
    }
    let newActionClass: ActionClass;
    try {
      newActionClass = await createActionClass.mutateAsync(updatedAction);
    } catch (error) {
      toast.error(errorMessage(t, error));
      return;
    }

    if (setLocalSurvey) {
      setLocalSurvey((prev) => ({
        ...prev,
        triggers: prev.triggers.concat({ actionClass: newActionClass }),
      }));
    }

    resetAllStates();
    toast.success(t("workspace.actions.action_created_successfully"));
  };

  const resetAllStates = () => {
    reset();
    setOpen(false);
  };

  return (
    <div>
      <FormProvider {...form}>
        <form onSubmit={handleSubmit(submitHandler)} aria-label="create-action-form">
          <div className="w-full space-y-4">
            <div className="w-3/5">
              <FormField
                name={`type`}
                control={control}
                render={({ field }) => (
                  <div>
                    <Label className="font-semibold">{t("workspace.actions.action_type")}</Label>
                    <TabToggle
                      id="type"
                      options={[
                        { value: "noCode", label: t("common.no_code") },
                        { value: "code", label: t("common.code") },
                      ]}
                      {...field}
                      defaultSelected={field.value}
                    />
                  </div>
                )}
              />
            </div>

            <ActionNameDescriptionFields
              control={control}
              isReadOnly={isReadOnly}
              nameInputId="actionNameInput"
              descriptionInputId="actionDescriptionInput"
            />

            {actionType === "code" ? (
              <CodeActionForm form={form} isReadOnly={isReadOnly} />
            ) : (
              <NoCodeActionForm form={form} isReadOnly={isReadOnly} />
            )}
          </div>
          <div className="sticky bottom-0 flex justify-end gap-x-2 bg-white pt-4">
            <Button type="button" variant="secondary" onClick={resetAllStates}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" loading={isSubmitting}>
              {t("workspace.actions.create_action")}
            </Button>
          </div>
        </form>
      </FormProvider>
    </div>
  );
};
