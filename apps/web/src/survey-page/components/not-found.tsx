import { HelpCircleIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

/** forma2's LinkSurveyNotFound, translated. An unknown id and a survey that is not live yet (a draft) read the same, so the page never reveals that a draft exists. */
export const LinkSurveyNotFound = () => {
  const { t } = useTranslation();
  return (
    <main className="flex h-full flex-col items-center justify-center bg-linear-to-br from-slate-200 to-slate-50 px-4 py-8 text-center">
      <div className="flex flex-col items-center gap-y-3 text-slate-300">
        <HelpCircleIcon className="size-20" aria-hidden="true" />
        <h1 className="text-4xl font-bold text-slate-800">{t("s.survey_not_available_heading")}</h1>
        <p className="text-lg leading-8 text-slate-500">{t("s.survey_not_available_description")}</p>
      </div>
    </main>
  );
};
