import { TrashIcon } from "lucide-react";
import { useMemo, useState } from "react";
import { FormProvider, useForm } from "react-hook-form";
import { toast } from "../../../lib/toast";
import { useTranslation } from "react-i18next";
import { TActionClass, TActionClassInput } from "@forma/types/action-classes";
import { errorMessage } from "../../../lib/errors";
import { buildActionObject } from "../../survey-editor/lib/action-builder";
import {
  createActionClassZodResolver,
  useActionClassKeys,
  validatePermissions,
} from "../../survey-editor/lib/action-utils";
import { ActionNameDescriptionFields } from "../../../components/ui/action-name-description-fields";
import { Button } from "../../../components/ui/button";
import { CodeActionForm } from "../../../components/ui/code-action-form";
import { DeleteDialog } from "../../../components/ui/delete-dialog";
import { NoCodeActionForm } from "../../../components/ui/no-code-action-form/index";
import { useDeleteActionClass, useUpdateActionClass } from "../api";
import { ActionSharedSurveysWarning } from "./action-shared-surveys-warning";

interface ActionSettingsTabProps {
  actionClass: TActionClass;
  actionClasses: TActionClass[];
  setOpen: (v: boolean) => void;
  isReadOnly: boolean;
  hideDelete?: boolean;
  currentSurveyId?: string;
  onActionUpdated?: (updatedAction: TActionClass) => void;
}

export const ActionSettingsTab = ({
  actionClass,
  actionClasses,
  setOpen,
  isReadOnly,
  hideDelete = false,
  currentSurveyId,
  onActionUpdated,
}: Readonly<ActionSettingsTabProps>) => {
  const { createdAt: _createdAt, updatedAt: _updatedAt, id: _id, ...restActionClass } = actionClass;
  const [openDeleteDialog, setOpenDeleteDialog] = useState(false);
  const { t } = useTranslation();
  const updateActionClass = useUpdateActionClass(actionClass.workspaceId);
  const deleteActionClass = useDeleteActionClass(actionClass.workspaceId);

  const actionClassNames = useMemo(
    () =>
      actionClasses.filter((action) => action.id !== actionClass.id).map((actionClass) => actionClass.name),
    [actionClass.id, actionClasses]
  );

  const actionClassKeys = useActionClassKeys(actionClasses).filter((key) => key !== actionClass.key);

  const form = useForm<TActionClassInput>({
    defaultValues: {
      ...restActionClass,
    },
    resolver: createActionClassZodResolver(actionClassNames, actionClassKeys, t),

    mode: "onChange",
  });

  const {
    handleSubmit,
    control,
    formState: { isDirty },
  } = form;

  const renderActionForm = () => {
    if (actionClass.type === "code") {
      return <CodeActionForm form={form} isReadOnly={true} />;
    }

    if (actionClass.type === "noCode") {
      return <NoCodeActionForm form={form} isReadOnly={isReadOnly} />;
    }

    return (
      <p className="text-sm text-slate-600">
        {t("workspace.actions.this_action_was_created_automatically_you_cannot_make_changes_to_it")}
      </p>
    );
  };

  const onSubmit = async (data: TActionClassInput) => {
    let updatedAction: TActionClassInput;
    try {
      validatePermissions(isReadOnly, t);
      updatedAction = buildActionObject(data, actionClass.workspaceId, t);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Unknown error occurred");
      return;
    }
    try {
      const result = await updateActionClass.mutateAsync({ id: actionClass.id, input: updatedAction });
      onActionUpdated?.(result);
      setOpen(false);
      toast.success(t("workspace.actions.action_updated_successfully"));
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  const handleDeleteAction = async () => {
    try {
      await deleteActionClass.mutateAsync(actionClass.id);
      toast.success(t("workspace.actions.action_deleted_successfully"));
      setOpen(false);
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  return (
    <div>
      <FormProvider {...form}>
        <form onSubmit={handleSubmit(onSubmit)}>
          <div className="max-h-[400px] w-full space-y-4 overflow-y-auto">
            {currentSurveyId ? (
              <ActionSharedSurveysWarning actionClassId={actionClass.id} currentSurveyId={currentSurveyId} />
            ) : null}

            <ActionNameDescriptionFields
              control={control}
              isReadOnly={isReadOnly}
              nameInputId="actionNameSettingsInput"
              descriptionInputId="actionDescriptionSettingsInput"
            />

            {renderActionForm()}
          </div>

          <div className="flex justify-between gap-x-2 border-slate-200 pt-4">
            <div className="flex items-center gap-x-2">
              {isReadOnly || hideDelete ? null : (
                <Button
                  type="button"
                  variant="destructive"
                  onClick={() => setOpenDeleteDialog(true)}
                  id="deleteActionModalTrigger">
                  <TrashIcon />
                  {t("common.delete")}
                </Button>
              )}
            </div>

            {isReadOnly ? null : (
              <div className="flex gap-x-2">
                <Button type="submit" loading={updateActionClass.isPending} disabled={!isDirty}>
                  {t("common.save_changes")}
                </Button>
              </div>
            )}
          </div>
        </form>
      </FormProvider>

      <DeleteDialog
        open={openDeleteDialog}
        setOpen={setOpenDeleteDialog}
        isDeleting={deleteActionClass.isPending}
        deleteWhat={t("common.action")}
        text={t("workspace.actions.delete_action_text")}
        onDelete={handleDeleteAction}
      />
    </div>
  );
};
