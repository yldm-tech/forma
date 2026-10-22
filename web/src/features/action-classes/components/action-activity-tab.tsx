import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { TActionClass } from "@forma/types/action-classes";
import { formatDateTimeForDisplay } from "../../../lib/survey/datetime";
import { ErrorComponent } from "../../../components/ui/error-component";
import { Label } from "../../../components/ui/label";
import { LoadingSpinner } from "../../../components/ui/loading-spinner";
import { actionClassSurveysQuery } from "../api";
import { ACTION_TYPE_ICON_LOOKUP } from "../lib/action-icon";

interface ActivityTabProps {
  actionClass: TActionClass;
}

export const ActionActivityTab = ({ actionClass }: ActivityTabProps) => {
  const { t, i18n } = useTranslation();
  const locale = i18n.resolvedLanguage ?? i18n.language ?? "en-US";
  const { data, isPending, isError } = useQuery(actionClassSurveysQuery(actionClass.id));

  if (isPending) return <LoadingSpinner />;
  if (isError) return <ErrorComponent />;
  const { activeSurveys, inactiveSurveys } = data;

  return (
    <div className="grid grid-cols-3 pb-2">
      <div className="col-span-2 space-y-4 pr-6">
        <div>
          <Label className="text-slate-500">{t("common.active_surveys")}</Label>
          {activeSurveys.length === 0 && <p className="text-sm text-slate-900">-</p>}
          {activeSurveys.map((surveyName) => (
            <p key={surveyName} className="text-sm text-slate-900">
              {surveyName}
            </p>
          ))}
        </div>
        <div>
          <Label className="text-slate-500">{t("common.inactive_surveys")}</Label>
          {inactiveSurveys.length === 0 && <p className="text-sm text-slate-900">-</p>}
          {inactiveSurveys.map((surveyName) => (
            <p key={surveyName} className="text-sm text-slate-900">
              {surveyName}
            </p>
          ))}
        </div>
      </div>
      <div className="col-span-1 space-y-3 rounded-lg border border-slate-100 bg-slate-50 p-2">
        <div>
          <Label className="text-xs font-normal text-slate-500">{t("common.created_at")}</Label>
          <p className="text-xs text-slate-700">{formatDateTimeForDisplay(actionClass.createdAt, locale)}</p>
        </div>{" "}
        <div>
          <Label className="text-xs font-normal text-slate-500">{t("common.updated_at")}</Label>
          <p className="text-xs text-slate-700">{formatDateTimeForDisplay(actionClass.updatedAt, locale)}</p>
        </div>
        <div>
          <Label className="block text-xs font-normal text-slate-500">{t("common.type")}</Label>
          <div className="mt-1 flex items-center">
            <div className="mr-1.5 size-4 text-slate-600">{ACTION_TYPE_ICON_LOOKUP[actionClass.type]}</div>
            <p className="text-sm text-slate-700 capitalize">{actionClass.type}</p>
          </div>
        </div>
      </div>
    </div>
  );
};
