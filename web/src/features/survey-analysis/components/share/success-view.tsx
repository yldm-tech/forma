import type { TSurvey } from "@forma/types/surveys/types";
import { Link } from "@tanstack/react-router";
import { BellRing, BlocksIcon, Share2Icon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { ShareSurveyLink } from "./share-survey-link";

/** forma2's SuccessView: the start view after publishing, with the link survey's link and what to do next. */
export const SuccessView = ({
  survey,
  workspaceId,
  surveyUrl,
  publicDomain,
  setSurveyUrl,
  locale,
  onShare,
  isReadOnly,
}: {
  survey: TSurvey;
  workspaceId: string;
  surveyUrl: string;
  publicDomain: string;
  setSurveyUrl: (url: string) => void;
  locale: string;
  onShare: () => void;
  isReadOnly: boolean;
}) => {
  const { t } = useTranslation();
  const tileClassName =
    "flex flex-col items-center gap-3 rounded-lg border border-slate-100 bg-white p-4 text-center text-sm text-slate-900 hover:border-slate-200 md:p-8";
  return (
    <div className="flex h-full max-w-full flex-col overflow-hidden">
      {survey.type === "link" && (
        <div className="flex w-full flex-col items-center justify-center gap-8 py-[100px] text-center">
          <p className="text-xl font-semibold text-slate-900">
            {t("workspace.surveys.summary.your_survey_is_public")} 🎉
          </p>
          <ShareSurveyLink
            survey={survey}
            surveyUrl={surveyUrl}
            publicDomain={publicDomain}
            setSurveyUrl={setSurveyUrl}
            locale={locale}
            enforceSurveyUrlWidth
            isReadOnly={isReadOnly}
          />
        </div>
      )}
      <div className="flex h-full flex-col items-center justify-center gap-8 rounded-b-lg bg-slate-50 px-8 py-4">
        <p className="text-sm font-medium text-slate-900">{t("workspace.surveys.summary.whats_next")}</p>
        <div className="grid grid-cols-3 gap-2">
          <button type="button" onClick={onShare} className={tileClassName}>
            <Share2Icon className="size-8 stroke-1 text-slate-900" />
            {t("workspace.surveys.summary.share_survey")}
          </button>
          <Link to="/account/settings/notifications" className={tileClassName}>
            <BellRing className="size-8 stroke-1 text-slate-900" />
            {t("workspace.surveys.summary.configure_alerts")}
          </Link>
          <Link to="/workspaces/$workspaceId/integrations" params={{ workspaceId }} className={tileClassName}>
            <BlocksIcon className="size-8 stroke-1 text-slate-900" />
            {t("workspace.surveys.summary.setup_integrations")}
          </Link>
        </div>
      </div>
    </div>
  );
};
