import * as Collapsible from "@radix-ui/react-collapsible";
import { Hand } from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  TSurvey,
  TSurveyEndScreenCard,
  TSurveyRedirectUrlCard,
  TSurveyWelcomeCard,
} from "@forma/types/surveys/types";
import { TUserLocale } from "@forma/types/user";
import { cn } from "../../../lib/cn";
import { ElementFormInput } from "./element-form-input/index";
import { FileInput } from "../../../components/ui/file-input/index";
import { Label } from "../../../components/ui/label";
import { Switch } from "../../../components/ui/switch";

interface EditWelcomeCardProps {
  localSurvey: TSurvey;
  setLocalSurvey: (survey: TSurvey) => void;
  setActiveElementId: (id: string | null) => void;
  activeElementId: string | null;
  isInvalid: boolean;
  locale: TUserLocale;
}

export const EditWelcomeCard = ({
  localSurvey,
  setLocalSurvey,
  setActiveElementId,
  activeElementId,
  isInvalid,
  locale,
}: EditWelcomeCardProps) => {
  const { t } = useTranslation();

  let open = activeElementId == "start";

  const setOpen = (e: boolean) => {
    if (e) {
      setActiveElementId("start");
    } else {
      setActiveElementId(null);
    }
  };

  const updateSurvey = (
    data: Partial<TSurveyEndScreenCard> | Partial<TSurveyRedirectUrlCard> | Partial<TSurveyWelcomeCard>
  ) => {
    setLocalSurvey({
      ...localSurvey,
      welcomeCard: {
        ...localSurvey.welcomeCard,
        ...data,
      },
    });
  };

  return (
    <div className={cn(open ? "shadow-lg" : "shadow-md", "group flex flex-row rounded-lg bg-white")}>
      <div
        className={cn(
          open ? "bg-slate-50" : "",
          "flex w-10 items-center justify-center rounded-l-lg border-t border-b border-l group-aria-expanded:rounded-bl-none",
          isInvalid ? "bg-red-400" : "bg-white group-hover:bg-slate-50"
        )}>
        <Hand className="size-4" />
      </div>
      <Collapsible.Root
        open={open}
        onOpenChange={setOpen}
        className="flex-1 rounded-r-lg border border-slate-200 transition-all duration-200 ease-in-out">
        {/* The trigger holds the title only; the on/off switch sits beside it, not inside the button. */}
        <div className="flex justify-between gap-4 rounded-r-lg p-4 hover:bg-slate-50">
          <Collapsible.CollapsibleTrigger className="min-w-0 grow cursor-pointer rounded-md text-left focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-slate-700">
            <div className="inline-flex">
              <div>
                <p className="text-sm font-semibold">{t("common.welcome_card")}</p>
                {!open && (
                  <p className="mt-1 truncate text-xs text-slate-500">
                    {localSurvey?.welcomeCard?.enabled ? t("common.shown") : t("common.hidden")}
                  </p>
                )}
              </div>
            </div>
          </Collapsible.CollapsibleTrigger>

          <div className="flex items-center gap-x-2">
            <Label htmlFor="welcome-toggle">
              {localSurvey?.welcomeCard?.enabled ? t("common.on") : t("common.off")}
            </Label>

            <Switch
              id="welcome-toggle"
              aria-label={t("common.welcome_card")}
              checked={localSurvey?.welcomeCard?.enabled}
              onClick={(e) => {
                e.stopPropagation();
                const newEnabledState = !localSurvey.welcomeCard?.enabled;
                updateSurvey({ enabled: newEnabledState });
                if (newEnabledState && !open) {
                  setActiveElementId("start");
                }
              }}
            />
          </div>
        </div>
        <Collapsible.CollapsibleContent className={`flex flex-col px-4 ${open && "pb-6"}`}>
          <form>
            <div className="mt-2">
              <Label htmlFor="companyLogo">{t("common.company_logo")}</Label>
            </div>
            <div className="mt-3 flex w-full items-center justify-center">
              <FileInput
                id="welcome-card-image"
                onFileUpload={(url: string[] | undefined) => {
                  if (url?.length && url[0]) {
                    updateSurvey({ fileUrl: url[0], videoUrl: undefined });
                  } else {
                    updateSurvey({ fileUrl: undefined, videoUrl: undefined });
                  }
                }}
                fileUrl={localSurvey?.welcomeCard?.fileUrl}
                workspaceId={localSurvey.workspaceId}
                maxSizeInMB={5}
              />
            </div>
            <div className="mt-3">
              <ElementFormInput
                id="headline"
                value={localSurvey.welcomeCard.headline}
                label={t("common.headline") + "*"}
                localSurvey={localSurvey}
                elementIdx={-1}
                isInvalid={isInvalid}
                updateSurvey={updateSurvey}
                locale={locale}
              />
            </div>
            <div className="mt-3">
              <ElementFormInput
                id="subheader"
                value={localSurvey.welcomeCard.subheader}
                label={t("workspace.surveys.edit.welcome_message")}
                localSurvey={localSurvey}
                elementIdx={-1}
                isInvalid={isInvalid}
                updateSurvey={updateSurvey}
                locale={locale}
              />
            </div>

            <div className="mt-3 flex justify-between gap-8">
              <div className="flex w-full gap-x-2">
                <div className="w-full">
                  <ElementFormInput
                    id="buttonLabel"
                    value={localSurvey.welcomeCard.buttonLabel}
                    localSurvey={localSurvey}
                    elementIdx={-1}
                    maxLength={48}
                    placeholder={t("common.next")}
                    isInvalid={isInvalid}
                    updateSurvey={updateSurvey}
                    label={t("workspace.surveys.edit.next_button_label")}
                    locale={locale}
                  />
                </div>
              </div>
            </div>
            <div className="mt-8 flex items-center">
              <div className="mr-2">
                <Switch
                  id="timeToFinish"
                  name="timeToFinish"
                  checked={localSurvey?.welcomeCard?.timeToFinish}
                  onCheckedChange={() =>
                    updateSurvey({ timeToFinish: !localSurvey.welcomeCard.timeToFinish })
                  }
                />
              </div>
              <div className="flex-column">
                <Label htmlFor="timeToFinish">{t("common.time_to_finish")}</Label>
                <div className="text-sm text-slate-500 dark:text-slate-400">
                  {t("workspace.surveys.edit.display_an_estimate_of_completion_time_for_survey")}
                </div>
              </div>
            </div>
            {localSurvey?.type === "link" && (
              <div className="mt-6 flex items-center">
                <div className="mr-2">
                  <Switch
                    id="showResponseCount"
                    name="showResponseCount"
                    checked={localSurvey?.welcomeCard?.showResponseCount}
                    onCheckedChange={() =>
                      updateSurvey({ showResponseCount: !localSurvey.welcomeCard.showResponseCount })
                    }
                  />
                </div>
                <div className="flex-column">
                  <Label htmlFor="showResponseCount">{t("common.show_response_count")}</Label>
                  <div className="text-sm text-slate-500 dark:text-slate-400">
                    {t("workspace.surveys.edit.display_number_of_responses_for_survey")}
                  </div>
                </div>
              </div>
            )}
          </form>
        </Collapsible.CollapsibleContent>
      </Collapsible.Root>
    </div>
  );
};
