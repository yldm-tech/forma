import { zodResolver } from "@hookform/resolvers/zod";
import { createId } from "@paralleldrive/cuid2";
import { PieChart, Trash2Icon } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { toast } from "../../../../lib/toast";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import {
  TSurveyQuota,
  TSurveyQuotaInput,
  TSurveyQuotaLogic,
  ZSurveyQuotaAction,
  ZSurveyQuotaInput,
} from "@forma/types/quota";
import { TSurvey } from "@forma/types/surveys/types";
import { errorMessage } from "../../../../lib/errors";
import { replaceHeadlineRecall } from "../../../../lib/survey/recall";
import { useCreateQuota, useUpdateQuota } from "../../api";
import { EndingCardSelector } from "./ending-card-selector";
import { getDefaultOperatorForElement } from "../../lib/utils";
import { replaceEndingCardHeadlineRecall } from "../../lib/utils";
import { getElementsFromBlocks } from "../../lib/client-utils";
import { Button } from "../../../../components/ui/button";
import { ConfirmationModal } from "../../../../components/ui/confirmation-modal";
import {
  Dialog,
  DialogBody,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../../../../components/ui/dialog";
import {
  FormControl,
  FormDescription,
  FormError,
  FormField,
  FormItem,
  FormLabel,
  FormProvider,
} from "../../../../components/ui/form";
import { Input } from "../../../../components/ui/input";
import {
  SelectMenu,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "../../../../components/ui/select-menu";
import { Switch } from "../../../../components/ui/switch";
import { QuotaConditionBuilder } from "./quota-condition-builder";

interface QuotaModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  survey: TSurvey;
  setQuotaToDelete: (quota: TSurveyQuota) => void;
  quota?: TSurveyQuota | null;
  onClose: () => void;
  duplicateQuota: (quota: TSurveyQuota) => void;
  hasResponses: boolean;
  quotaResponseCount: number;
}

export const QuotaModal = ({
  open,
  onOpenChange,
  survey,
  quota,
  setQuotaToDelete,
  onClose,
  duplicateQuota,
  hasResponses,
  quotaResponseCount,
}: QuotaModalProps) => {
  const createQuota = useCreateQuota(survey.id);
  const updateQuota = useUpdateQuota(survey.id);
  const isEditing = !!quota;
  const { t } = useTranslation();
  const [openConfirmationModal, setOpenConfirmationModal] = useState(false);
  const [openConfirmChangesInInclusionCriteria, setOpenConfirmChangesInInclusionCriteria] = useState(false);

  // Transform survey to replace recall: with actual question headlines
  const transformedSurvey = useMemo(() => {
    let modifiedSurvey = replaceHeadlineRecall(survey, "default");
    modifiedSurvey = replaceEndingCardHeadlineRecall(modifiedSurvey, "default");

    return modifiedSurvey;
  }, [survey]);

  const elements = useMemo(() => getElementsFromBlocks(transformedSurvey.blocks), [transformedSurvey.blocks]);

  const defaultValues = useMemo(() => {
    const firstElement = elements[0];
    return {
      name: quota?.name || "",
      limit: quota?.limit || 1,
      logic: quota?.logic || {
        connector: "and",
        conditions: [
          {
            id: createId(),
            leftOperand: { type: "element", value: firstElement?.id },
            operator: firstElement ? getDefaultOperatorForElement(firstElement, t) : "equals",
          },
        ],
      },
      action: quota?.action || "endSurvey",
      endingCardId: quota?.endingCardId || survey.endings[0]?.id || null,
      countPartialSubmissions: quota?.countPartialSubmissions || false,
      surveyId: survey.id,
    };
  }, [quota, survey, elements, t]);

  const form = useForm<TSurveyQuotaInput>({
    defaultValues,
    resolver: zodResolver(
      quotaResponseCount > 0
        ? ZSurveyQuotaInput.safeExtend({
            limit: z.number().min(quotaResponseCount, {
              error: t(
                "workspace.surveys.edit.quotas.limit_must_be_greater_than_or_equal_to_the_number_of_responses",
                { value: quotaResponseCount }
              ),
            }),
          })
        : ZSurveyQuotaInput
    ),
    mode: "onSubmit",
    criteriaMode: "all",
  });

  const {
    handleSubmit,
    reset,
    control,
    formState: { isSubmitting, isDirty, errors, isValid, isSubmitted },
  } = form;

  // Watch form values for conditional logic
  const action = useWatch({ control, name: "action" });

  useEffect(() => {
    if (open) {
      form.reset(defaultValues);
    }
  }, [open, defaultValues, form]);

  const handleCreateQuota = useCallback(
    async (quota: TSurveyQuotaInput) => {
      try {
        await createQuota.mutateAsync(quota);
        toast.success(t("workspace.surveys.edit.quotas.quota_created_successfull_toast"));
        onClose();
      } catch (error) {
        toast.error(errorMessage(t, error));
      }
    },
    [t, createQuota, onClose]
  );

  const handleUpdateQuota = useCallback(
    async (updatedQuota: TSurveyQuotaInput, quotaId: string) => {
      try {
        await updateQuota.mutateAsync({ id: quotaId, input: updatedQuota });
        toast.success(t("workspace.surveys.edit.quotas.quota_updated_successfull_toast"));
        onClose();
      } catch (error) {
        toast.error(errorMessage(t, error));
      }
      setOpenConfirmChangesInInclusionCriteria(false);
    },
    [t, updateQuota, onClose]
  );
  const submitQuota = async (data: TSurveyQuotaInput) => {
    const trimmedName = data.name.trim();
    if (data.limit < quotaResponseCount) {
      form.setError("limit", {
        message: t(
          "workspace.surveys.edit.quotas.limit_must_be_greater_than_or_equal_to_the_number_of_responses"
        ),
      });
      return;
    }

    let payload = {
      name: trimmedName || t("workspace.surveys.edit.quotas.new_quota"),
      limit: data.limit,
      logic: data.logic,
      action: data.action,
      endingCardId: data.endingCardId || null,
      countPartialSubmissions: data.countPartialSubmissions,
      surveyId: survey.id,
    };

    if (isEditing) {
      await handleUpdateQuota(payload, quota.id);
    } else {
      await handleCreateQuota(payload);
    }
  };

  // Form submission handler with confirmation logic
  const onSubmit = async (data: TSurveyQuotaInput) => {
    if (isEditing) {
      const checkIfInclusionCriteriaHasChanged =
        hasResponses && JSON.stringify(form.getValues("logic")) !== JSON.stringify(quota.logic);
      if (checkIfInclusionCriteriaHasChanged && isValid) {
        setOpenConfirmChangesInInclusionCriteria(true);
        return;
      }
    }
    await submitQuota(data);
  };

  const handleConditionsChange = useCallback(
    (newConditions: TSurveyQuotaLogic) => {
      form.setValue("logic", newConditions, { shouldDirty: true, shouldValidate: true });
    },
    [form]
  );

  const quotaActions = [
    {
      label: t("workspace.surveys.edit.quotas.end_survey_for_matching_participants"),
      value: ZSurveyQuotaAction.enum.endSurvey,
    },
    {
      label: t("workspace.surveys.edit.quotas.continue_survey_normally"),
      value: ZSurveyQuotaAction.enum.continueSurvey,
    },
  ];

  const handleClose = () => {
    if (isDirty) {
      setOpenConfirmationModal(true);
    } else {
      onClose();
    }
  };

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <form onSubmit={handleSubmit(onSubmit)} className="space-y-6">
        <FormProvider {...form}>
          <DialogHeader>
            <div className="flex items-center gap-2">
              <PieChart className="size-4" />
              <div className="flex flex-col">
                <DialogTitle>
                  {isEditing
                    ? t("workspace.surveys.edit.quotas.edit_quota")
                    : t("workspace.surveys.edit.quotas.new_quota")}
                </DialogTitle>
                <DialogDescription>{t("common.quotas_description")}</DialogDescription>
              </div>
            </div>
          </DialogHeader>

          <DialogBody className="space-y-6 px-1">
            {/* Quota Name Field */}
            <FormField
              control={control}
              name="name"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("common.label")}</FormLabel>
                  <FormControl>
                    <Input
                      {...field}
                      placeholder={
                        isEditing
                          ? t("workspace.surveys.edit.quotas.quota_name_placeholder")
                          : t("workspace.surveys.edit.quotas.new_quota")
                      }
                      className="bg-white"
                      autoFocus={!isEditing}
                    />
                  </FormControl>
                  {errors.name?.message && <FormError>{errors.name.message}</FormError>}
                </FormItem>
              )}
            />

            {/* Quota Limit Field */}
            <FormField
              control={control}
              name="limit"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("workspace.surveys.edit.quotas.response_limit")}</FormLabel>
                  <FormControl>
                    <Input
                      {...field}
                      type="number"
                      className="w-32 bg-white"
                      onChange={(e) => {
                        const value = e.target.value;
                        field.onChange(value === "" ? 1 : parseInt(value, 10));
                      }}
                    />
                  </FormControl>
                  {errors.limit?.message && <FormError>{errors.limit.message}</FormError>}
                </FormItem>
              )}
            />

            {/* Inclusion Criteria Field */}
            <FormField
              control={control}
              name="logic"
              render={({ field }) => (
                <FormItem>
                  <div className="space-y-4 rounded-lg bg-slate-50 p-3">
                    <label className="text-sm font-medium text-slate-800">
                      {t("workspace.surveys.edit.quotas.inclusion_criteria")}
                    </label>
                    <FormControl>
                      {field.value && (
                        <QuotaConditionBuilder
                          survey={transformedSurvey}
                          conditions={field.value}
                          onChange={handleConditionsChange}
                          quotaErrors={errors}
                          isSubmitted={isSubmitted}
                        />
                      )}
                    </FormControl>
                  </div>
                </FormItem>
              )}
            />

            {/* Quota Action Fields */}
            <FormField
              control={control}
              name="action"
              render={({ field }) => (
                <FormItem className="space-y-2">
                  <FormLabel>{t("workspace.surveys.edit.quotas.when_quota_has_been_reached")}</FormLabel>
                  <div className="grid grid-cols-1 gap-2 md:grid-cols-2">
                    <div className="space-y-2">
                      <FormControl>
                        <SelectMenu value={field.value} onValueChange={field.onChange}>
                          <SelectTrigger className="bg-white">
                            <SelectValue placeholder="Select action" />
                          </SelectTrigger>
                          <SelectContent>
                            {quotaActions.map((action) => (
                              <SelectItem key={action.value} value={action.value}>
                                {action.label}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </SelectMenu>
                      </FormControl>
                    </div>

                    {action === "endSurvey" && (
                      <FormField
                        control={control}
                        name="endingCardId"
                        render={({ field: endingCardField }) => (
                          <div className="space-y-2">
                            <FormControl>
                              <EndingCardSelector
                                survey={survey}
                                value={endingCardField.value || ""}
                                onChange={(value) => {
                                  form.setValue("endingCardId", value, {
                                    shouldDirty: true,
                                    shouldValidate: true,
                                  });
                                  form.setValue("action", "endSurvey", {
                                    shouldDirty: true,
                                    shouldValidate: true,
                                  });
                                }}
                              />
                            </FormControl>
                          </div>
                        )}
                      />
                    )}
                  </div>
                  {errors.action?.message && <FormError>{errors.action.message}</FormError>}
                </FormItem>
              )}
            />

            {/* Count Partial Submissions Field */}
            <FormField
              control={control}
              name="countPartialSubmissions"
              render={({ field }) => (
                <FormItem className="flex flex-row items-start gap-x-3 gap-y-0">
                  <FormControl>
                    <Switch checked={field.value} onCheckedChange={field.onChange} />
                  </FormControl>
                  <div className="space-y-1 leading-none">
                    <FormLabel className="cursor-pointer">
                      {t("workspace.surveys.edit.quotas.count_partial_submissions")}
                    </FormLabel>
                    <FormDescription>
                      {t("workspace.surveys.edit.quotas.count_partial_submissions_description")}
                    </FormDescription>
                  </div>
                </FormItem>
              )}
            />
          </DialogBody>

          {/* Footer */}
          <DialogFooter>
            <div className="flex w-full justify-between gap-2">
              <Button
                type="button"
                variant="destructive"
                onClick={() => {
                  if (quota) {
                    setQuotaToDelete(quota);
                  }
                }}
                className="flex items-center gap-2"
                disabled={isSubmitting || !isEditing}>
                <Trash2Icon className="size-4" />
                {t("common.delete")}
              </Button>
              <div className="flex gap-2">
                <Button type="button" variant="outline" onClick={handleClose} disabled={isSubmitting}>
                  {t("common.cancel")}
                </Button>
                <Button type="submit" loading={isSubmitting} disabled={isSubmitting || !isDirty}>
                  {t("common.save")}
                </Button>
              </div>
            </div>
          </DialogFooter>
          <ConfirmationModal
            title={t("workspace.surveys.edit.quotas.confirm_quota_changes")}
            open={openConfirmationModal}
            buttonVariant="default"
            buttonLoading={isSubmitting}
            setOpen={setOpenConfirmationModal}
            onConfirm={() => {
              setOpenConfirmationModal(false);
              void form.handleSubmit(submitQuota)();
            }}
            body={t("workspace.surveys.edit.quotas.confirm_quota_changes_body")}
            buttonText={t("common.save")}
            cancelButtonText={t("common.discard")}
            onCancel={() => {
              reset();
              onOpenChange(false);
            }}
          />
          <ConfirmationModal
            open={openConfirmChangesInInclusionCriteria}
            setOpen={setOpenConfirmChangesInInclusionCriteria}
            title={t("workspace.surveys.edit.quotas.change_quota_for_public_survey")}
            description={t("workspace.surveys.edit.quotas.save_changes_confirmation_text")}
            body={t("workspace.surveys.edit.quotas.save_changes_confirmation_body")}
            buttonText={t("common.continue")}
            buttonVariant="default"
            onConfirm={form.handleSubmit(submitQuota)}
            secondaryButton={{
              text: t("workspace.surveys.edit.quotas.duplicate_quota"),
              variant: "secondary",
              onAction: () => {
                if (quota) {
                  const updatedQuota = {
                    ...quota,
                    ...form.getValues(),
                  };
                  duplicateQuota(updatedQuota);
                  onOpenChange(false);
                  setOpenConfirmChangesInInclusionCriteria(false);
                }
              },
            }}
          />
        </FormProvider>
      </form>
    </Dialog>
  );
};
