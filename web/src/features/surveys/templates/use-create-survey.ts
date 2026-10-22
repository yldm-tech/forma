import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { toast } from "../../../lib/toast";
import { type CreateFromTemplateInput, createSurveyFromTemplate, v3ErrorMessage } from "../api";
import { surveyKeys } from "../list/query";

/** Where a survey just created from a template is opened: its editor, in the CX mode when the onboarding flow created it. */
export const surveyEditorPath = (workspaceId: string, surveyId: string, mode?: "cx"): string =>
  `/workspaces/${workspaceId}/surveys/${surveyId}/edit${mode ? `?mode=${mode}` : ""}`;

/** forma2's useCreateSurveyFromTemplate with the handling every caller shared: open the new survey's editor, or show why it could not be created. */
export const useCreateSurveyFromTemplate = (mode?: "cx") => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const mutation = useMutation({
    mutationFn: createSurveyFromTemplate,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: surveyKeys.lists() }),
  });
  const create = async (input: CreateFromTemplateInput) => {
    try {
      const survey = await mutation.mutateAsync(input);
      await navigate({ href: surveyEditorPath(input.workspaceId, survey.id, mode) });
    } catch (error) {
      toast.error(v3ErrorMessage(error, t("common.something_went_wrong_please_try_again")));
    }
  };
  return { create, isPending: mutation.isPending };
};
