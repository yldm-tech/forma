import type { TSurvey } from "@forma/types/surveys/types";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ArchiveIcon, ChevronDownIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "../../../components/ui/dropdown-menu";
import { errorMessage } from "../../../lib/errors";
import { toast } from "../../../lib/toast";
import { forgetEditorSurvey } from "../../survey-editor/api";
import { surveysApi } from "../../surveys/api";
import type { SurveyStatus } from "../../surveys/list/filters";
import { surveyKeys } from "../../surveys/list/query";
import { SurveyStatusIndicator } from "../../surveys/list/survey-status-indicator";
import { analysisSurveyQuery } from "../api";

const CHANGEABLE: Exclude<SurveyStatus, "draft">[] = ["inProgress", "paused", "completed"];

/** forma2's SurveyStatusDropdown: the live status of a published survey, changeable in place; an archived survey shows "Archived", disabled, and a draft only its name. */
export const SurveyStatusDropdown = ({ survey }: { survey: TSurvey }) => {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const isScheduled = survey.status === "paused" && survey.publishOn !== null;

  const statusLabel = (status: SurveyStatus, scheduled: boolean): string => {
    switch (status) {
      case "inProgress":
        return t("common.in_progress");
      case "paused":
        return scheduled ? t("common.scheduled") : t("common.paused");
      case "completed":
        return t("common.completed");
      case "draft":
        return t("common.draft");
    }
  };

  const changeStatus = useMutation({
    mutationFn: async (status: SurveyStatus) => {
      await surveysApi.updateStatus(survey.id, status);
      // The server may land elsewhere than asked (a survey with a future publish date pauses as scheduled), so the toast reads the stored survey.
      return queryClient.fetchQuery({ ...analysisSurveyQuery(survey.id), staleTime: 0 });
    },
    onSuccess: (updated) => {
      const resultScheduled = updated.status === "paused" && updated.publishOn !== null;
      const messages: Partial<Record<TSurvey["status"], string>> = {
        inProgress: t("common.survey_live"),
        paused: resultScheduled ? t("common.survey_scheduled") : t("common.survey_paused"),
        completed: t("common.survey_completed"),
      };
      const message = messages[updated.status];
      if (message) toast.success(message);
    },
    onError: (error) => toast.error(errorMessage(t, error)),
    onSettled: () => {
      forgetEditorSurvey(queryClient, survey.id);
      return queryClient.invalidateQueries({ queryKey: surveyKeys.lists() });
    },
  });

  const triggerClassName =
    "flex h-9 w-[170px] items-center justify-between rounded-md border border-slate-300 bg-white px-3 text-sm text-slate-700 md:w-[200px] disabled:cursor-not-allowed disabled:opacity-50";

  if (survey.archivedAt) {
    return (
      <button type="button" disabled className={triggerClassName}>
        <span className="flex items-center">
          <span className="rounded-full bg-slate-300 p-1">
            <ArchiveIcon className="size-3 text-slate-600" />
          </span>
          <span className="ml-2">{t("common.archived")}</span>
        </span>
        <ChevronDownIcon className="size-4 opacity-50" />
      </button>
    );
  }

  if (survey.status === "draft") {
    return (
      <div className="flex items-center">
        <p className="text-sm text-slate-600 italic">{t("common.draft")}</p>
      </div>
    );
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild disabled={changeStatus.isPending}>
        <button type="button" className={triggerClassName} aria-label={t("common.status")}>
          <span className="flex items-center">
            <SurveyStatusIndicator status={survey.status} isScheduled={isScheduled} />
            <span className="ml-2">{statusLabel(survey.status, isScheduled)}</span>
          </span>
          <ChevronDownIcon className="size-4 opacity-50" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-[200px] bg-white">
        {CHANGEABLE.map((status) => (
          <DropdownMenuItem
            key={status}
            className="cursor-pointer font-normal"
            onClick={() => {
              if (status !== survey.status) changeStatus.mutate(status);
            }}>
            <span className="flex items-center gap-3">
              <SurveyStatusIndicator status={status} isScheduled={status === "paused" && isScheduled} />
              {statusLabel(status, status === "paused" && isScheduled)}
            </span>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
};
