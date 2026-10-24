import { getLinkSurveyCardMaxWidth } from "@forma/types/styling";
import type { TWorkspaceStyling } from "@forma/types/workspace";
import { RotateCcwIcon } from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { SurveyInline } from "../../components/survey/survey-inline";
import { cn } from "../../lib/cn";
import { previewSurvey } from "../../lib/preview-survey";
import type { Workspace } from "../../lib/queries/workspaces";

const PLACEMENT_CLASSES: Record<Workspace["placement"], string> = {
  bottomRight: "bottom-3 right-3",
  topRight: "top-3 right-3",
  topLeft: "top-3 left-3",
  bottomLeft: "bottom-3 left-3",
  center: "top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2",
};

/**
 * forma2's ThemeStylingPreviewSurvey: the preview survey rendered by the respondent runtime with the theme being edited, as a link survey on its page background or as an app survey in a mock web app at the workspace's placement, with a restart and a link/app switch.
 */
export const ThemePreview = ({
  workspace,
  styling,
  previewType,
  setPreviewType,
}: {
  workspace: Workspace;
  styling: TWorkspaceStyling;
  previewType: "link" | "app";
  setPreviewType: (type: "link" | "app") => void;
}) => {
  const { t } = useTranslation();
  const [restarts, setRestarts] = useState(0);
  const isApp = previewType === "app";
  const isCardless = !isApp && styling.cardArrangement?.linkSurveys === "cardless";
  const survey = useMemo(
    () => previewSurvey(workspace.name, t, previewType),
    [workspace.name, t, previewType]
  );
  const surveyProps = useMemo(
    () => ({
      survey,
      styling,
      isPreviewMode: true,
      // The preview renders inline in the settings page, not in an iframe, so the runtime would otherwise focus its first control on every card and pull focus away from the form beside it.
      autoFocus: false,
      isRedirectDisabled: true,
      isBrandingEnabled: isApp ? workspace.inAppSurveyBranding : workspace.linkSurveyBranding,
      onFileUpload: async (file: { name: string }) => file.name,
      languageCode: "default",
      ...(isApp
        ? { isCardBorderVisible: !styling.highlightBorderColor?.light, placement: workspace.placement }
        : { responseCount: 42 }),
    }),
    [survey, styling, isApp, workspace]
  );
  const background = styling.background;
  const pageBackground =
    !isApp && background?.bgType === "color" && background.bg
      ? { backgroundColor: background.bg, filter: `brightness(${background.brightness ?? 100}%)` }
      : undefined;

  return (
    <div className="flex h-full w-full flex-col items-center">
      <div
        className={cn(
          "relative z-10 flex h-[700px] w-5/6 flex-col rounded-lg border border-slate-300 shadow-xl",
          isApp ? "bg-slate-200" : "overflow-y-auto bg-white"
        )}>
        <div className="flex h-auto w-full items-center rounded-t-lg bg-slate-100 py-2">
          <div className="ml-6 flex gap-x-2">
            <div className="size-3 rounded-full bg-red-500" />
            <div className="size-3 rounded-full bg-amber-500" />
            <div className="size-3 rounded-full bg-emerald-500" />
          </div>
          <div className="mx-4 flex w-full items-center justify-between font-mono text-sm text-slate-400">
            <p>{isApp ? t("workspace.surveys.edit.your_web_app") : t("common.preview")}</p>
            <button
              type="button"
              className="flex items-center gap-1 rounded-md px-2 py-1 text-slate-500 hover:bg-slate-200"
              onClick={() => setRestarts((n) => n + 1)}>
              {t("common.restart")}
              <RotateCcwIcon className="size-3" />
            </button>
          </div>
        </div>
        <div className="relative flex min-h-0 w-full flex-1 flex-col overflow-hidden rounded-b-lg">
          {/* The brightness applies to the page background only, as forma2's MediaBackground drew it beneath the survey. */}
          {pageBackground && <div aria-hidden="true" className="absolute inset-0" style={pageBackground} />}
          {isApp ? (
            <div
              className={cn("absolute w-[360px] overflow-hidden", PLACEMENT_CLASSES[workspace.placement])}
              style={{
                backgroundColor: styling.cardBackgroundColor?.light,
                borderRadius: styling.roundness ?? 8,
              }}>
              <SurveyInline key={`app-${restarts}`} options={surveyProps} />
            </div>
          ) : (
            <div
              className={cn(
                "relative mx-auto flex w-full flex-1",
                isCardless ? "flex-col" : "items-center p-4"
              )}
              style={
                isCardless ? undefined : { maxWidth: getLinkSurveyCardMaxWidth(styling.linkSurveyCardWidth) }
              }>
              <SurveyInline key={`link-${restarts}`} options={surveyProps} />
            </div>
          )}
        </div>
      </div>
      <div className="mt-2 flex rounded-full border-2 border-slate-300 p-1">
        <button
          type="button"
          aria-pressed={!isApp}
          className={cn("cursor-pointer px-3 py-1 text-sm", !isApp && "rounded-full bg-slate-200")}
          onClick={() => setPreviewType("link")}>
          {t("common.link_survey")}
        </button>
        <button
          type="button"
          aria-pressed={isApp}
          className={cn("cursor-pointer px-3 py-1 text-sm", isApp && "rounded-full bg-slate-200")}
          onClick={() => setPreviewType("app")}>
          {t("common.app_survey")}
        </button>
      </div>
    </div>
  );
};
