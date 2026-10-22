import { getLanguageLabel } from "@forma/i18n-utils/utils";
import type { TSurvey } from "@forma/types/surveys/types";
import { Copy, Languages, SquareArrowOutUpRight } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../../components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "../../../../components/ui/dropdown-menu";
import { Input } from "../../../../components/ui/input";
import { copyText } from "../../../../lib/clipboard";
import { cn } from "../../../../lib/cn";
import { getEnabledLanguages } from "../../../../lib/survey/i18n";
import { useSingleUseId } from "./use-single-use-id";

/** forma2's getSurveyUrl: the survey's public link, in a language other than the default through ?lang=. */
export const getSurveyUrl = (survey: Pick<TSurvey, "id">, publicDomain: string, language: string): string => {
  const url = `${publicDomain}/s/${survey.id}`;
  return language === "default" ? url : `${url}?lang=${language}`;
};

const LanguageDropdown = ({
  survey,
  setLanguage,
  locale,
}: {
  survey: TSurvey;
  setLanguage: (language: string) => void;
  locale: string;
}) => {
  const { t } = useTranslation();
  const enabledLanguages = getEnabledLanguages(survey.languages ?? []);
  if (enabledLanguages.length <= 1) return null;

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="secondary"
          title={t("common.select_language")}
          aria-label={t("common.select_language")}>
          <Languages className="size-5" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        className="max-h-64 max-w-48 overflow-auto bg-slate-900 p-1 text-sm text-white"
        align="start">
        {enabledLanguages.map((surveyLanguage) => (
          <DropdownMenuItem
            key={surveyLanguage.language.code}
            className="w-full truncate rounded-md p-2 text-start text-white hover:cursor-pointer hover:bg-slate-700 focus:bg-slate-700"
            onSelect={() => setLanguage(surveyLanguage.language.code)}>
            {getLanguageLabel(surveyLanguage.language.code, locale)}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
};

/** forma2's ShareSurveyLink: the survey's link with its language picker, copy and preview. */
export const ShareSurveyLink = ({
  survey,
  surveyUrl,
  publicDomain,
  setSurveyUrl,
  locale,
  enforceSurveyUrlWidth = false,
  isReadOnly,
}: {
  survey: TSurvey;
  surveyUrl: string;
  publicDomain: string;
  setSurveyUrl: (url: string) => void;
  locale: string;
  enforceSurveyUrlWidth?: boolean;
  isReadOnly: boolean;
}) => {
  const { t } = useTranslation();
  const { refreshSingleUseId } = useSingleUseId(survey, isReadOnly);

  const getPreviewUrl = async () => {
    const previewUrl = new URL(surveyUrl);
    if (survey.singleUse?.enabled) {
      const singleUseLinkParams = await refreshSingleUseId();
      if (singleUseLinkParams) {
        previewUrl.searchParams.set("suId", singleUseLinkParams.suId);
        if (singleUseLinkParams.suToken) previewUrl.searchParams.set("suToken", singleUseLinkParams.suToken);
      }
    }
    previewUrl.searchParams.set("preview", "true");
    return previewUrl.toString();
  };

  return (
    <div className="flex max-w-full items-center justify-center gap-2">
      {surveyUrl ? (
        <Input
          data-testid="survey-url-input"
          className={cn(
            "h-9 w-full rounded-lg border bg-white px-3 py-1 text-ellipsis text-slate-800 caret-transparent",
            enforceSurveyUrlWidth && "min-w-96"
          )}
          value={surveyUrl}
          readOnly
          aria-label={t("common.url")}
        />
      ) : (
        <div
          className={cn(
            "h-9 w-full animate-pulse rounded-lg bg-slate-100 px-3 py-1",
            enforceSurveyUrlWidth && "min-w-96"
          )}
        />
      )}
      <div className="flex items-center justify-center gap-x-2">
        <LanguageDropdown
          survey={survey}
          setLanguage={(language) => setSurveyUrl(getSurveyUrl(survey, publicDomain, language))}
          locale={locale}
        />
        <Button
          disabled={!surveyUrl}
          variant="secondary"
          title={t("workspace.surveys.copy_survey_link_to_clipboard")}
          aria-label={t("workspace.surveys.copy_survey_link_to_clipboard")}
          onClick={() => void copyText(surveyUrl, t)}>
          {t("common.copy")}
          <Copy />
        </Button>
        <Button
          title={t("workspace.surveys.preview_survey_in_a_new_tab")}
          aria-label={t("workspace.surveys.preview_survey_in_a_new_tab")}
          disabled={!surveyUrl}
          onClick={async () => {
            const url = await getPreviewUrl();
            window.open(url, "_blank", "noopener");
          }}>
          {t("common.preview")}
          <SquareArrowOutUpRight />
        </Button>
      </div>
    </div>
  );
};
