import { AlertCircleIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

/** What a survey page shows when the server failed to build it. */
export const PageError = () => {
  const { t } = useTranslation();
  return (
    <div className="flex h-full flex-col items-center justify-center bg-linear-to-br from-slate-200 to-slate-50 py-8 text-center">
      <div className="flex flex-col items-center gap-y-3 text-slate-300">
        <AlertCircleIcon className="size-20" />
        <h1 className="text-2xl font-bold text-slate-800">
          {t("common.something_went_wrong_please_try_again")}
        </h1>
      </div>
    </div>
  );
};
