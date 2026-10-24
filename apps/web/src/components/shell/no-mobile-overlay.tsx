import { Maximize2Icon, SmartphoneIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

/** forma2's NoMobileOverlay: below the sm breakpoint the signed-in app is covered by a note that it is made for bigger screens, while the surveys it builds work everywhere. */
export const NoMobileOverlay = () => {
  const { t } = useTranslation();
  return (
    <div className="fixed inset-0 z-9999 sm:hidden" data-testid="no-mobile-overlay">
      <div className="absolute inset-0 bg-slate-50" />
      <div className="relative mx-auto flex h-full max-w-xl flex-col items-center justify-center px-4 py-16 text-center">
        <div className="relative size-16">
          <SmartphoneIcon className="size-16 text-slate-500" aria-hidden="true" />
          <Maximize2Icon
            className="absolute top-1/3 left-1/2 size-5 -translate-x-1/2 -translate-y-1/3 text-slate-500"
            aria-hidden="true"
          />
        </div>
        <h1 className="mt-2 text-2xl font-bold text-zinc-900">{t("common.mobile_overlay_title")}</h1>
        <p className="mt-2 text-base text-zinc-600">{t("common.mobile_overlay_app_works_best_on_desktop")}</p>
        <p className="mt-2 text-base text-zinc-600">{t("common.mobile_overlay_surveys_look_good")}</p>
      </div>
    </div>
  );
};
