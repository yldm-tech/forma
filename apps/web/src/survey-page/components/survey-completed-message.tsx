import { CheckCircle2Icon } from "lucide-react";
import { useTranslation } from "react-i18next";
import footerLogo from "../assets/footerlogo.svg";
import type { TCompletedState } from "../lib/state";

const SURVEY_COMPLETED_URL =
  "https://forma.yldm.ai?utm_source=forma-app&utm_medium=survey&utm_campaign=survey_completed";

/** forma2's SurveyCompletedMessage: a single-use link whose response is finished. */
export const SurveyCompletedMessage = ({ singleUse, workspace }: TCompletedState) => {
  const { t } = useTranslation();
  return (
    <div className="flex min-h-screen flex-col items-center justify-between bg-linear-to-tr from-slate-200 to-slate-50 py-8 text-center">
      <div className="my-auto flex flex-col items-center gap-y-3 text-slate-300">
        <CheckCircle2Icon className="size-20" />
        <h1 className="text-4xl font-bold text-slate-800">
          {singleUse?.heading ?? t("s.survey_already_answered_heading")}
        </h1>
        <p className="text-lg leading-10 text-slate-500">
          {singleUse?.subheading ?? t("s.survey_already_answered_subheading")}
        </p>
      </div>
      {(!workspace || workspace.linkSurveyBranding) && (
        <div>
          <a href={SURVEY_COMPLETED_URL}>
            <img src={footerLogo} alt="Brand logo" className="mx-auto w-40" />
          </a>
        </div>
      )}
    </div>
  );
};
