import type { TResponseData, TResponseVariables } from "@forma/types/responses";
import type { TSurveyElement } from "@forma/types/surveys/elements";
import { getTextContent } from "@forma/types/surveys/validation";
import { CheckCircle2Icon, ChevronsDownIcon, XCircleIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { TooltipRenderer } from "../../../../components/ui/tooltip";
import { getLocalizedValue } from "../../../../lib/survey/i18n";
import { parseRecallInfo } from "../../../../lib/survey/recall";
import { getSurveyDateFormatMap } from "../../lib/response-display";

/** forma2's ElementSkip: the welcome card, a run of skipped elements, or where an unfinished response stopped. */
export const ElementSkip = ({
  skippedElements,
  status,
  elements,
  isFirstElementAnswered,
  recallValues,
  variables,
  locale,
}: {
  skippedElements: string[] | undefined;
  status: "welcomeCard" | "skipped" | "aborted";
  elements: TSurveyElement[];
  isFirstElementAnswered?: boolean;
  /** Recall's lookup map: the answers with the survey's readable reserved-field values under them. */
  recallValues: TResponseData;
  variables: TResponseVariables;
  locale: string;
}) => {
  const { t } = useTranslation();
  const dateFormats = getSurveyDateFormatMap(elements);
  if (!skippedElements) return null;

  const headline = (elementId: string) =>
    getTextContent(
      parseRecallInfo(
        getLocalizedValue(
          elements.find((element) => element.id === elementId)?.headline ?? { default: "" },
          "default"
        ),
        recallValues,
        variables,
        false,
        locale,
        dateFormats
      )
    );

  return (
    <div className="my-2 flex w-full px-2 text-sm text-slate-400">
      {status === "welcomeCard" && (
        <div className="mb-2 flex">
          <div
            className={`relative flex ${isFirstElementAnswered ? "h-full" : "h-[200%]"} w-0.5 items-center justify-center`}
            style={{
              background:
                "repeating-linear-gradient(rgb(148, 163, 184), rgb(148, 163, 184) 5px, transparent 5px, transparent 8px)",
            }}>
            <CheckCircle2Icon className="absolute top-0 w-6 min-w-6 rounded-full bg-white p-0.25 text-slate-400" />
          </div>
          <div className="ml-6 flex flex-col text-slate-700">{t("common.welcome_card")}</div>
        </div>
      )}
      {status === "skipped" && (
        <div className="flex">
          <div
            className="flex w-0.5 items-center justify-center"
            style={{
              background:
                "repeating-linear-gradient(to bottom, rgb(148 163 184), rgb(148 163 184) 8px, transparent 5px, transparent 15px)",
            }}>
            {skippedElements.length > 1 && (
              <TooltipRenderer
                tooltipContent={
                  <p data-testid="tooltip-respondent_skipped_questions">
                    {t("workspace.surveys.responses.respondent_skipped_questions")}
                  </p>
                }>
                <ChevronsDownIcon className="w-5 min-w-5 rounded-full bg-slate-400 p-0.5 text-white" />
              </TooltipRenderer>
            )}
          </div>
          <div className="ml-6 flex flex-col">
            {skippedElements.map((elementId) => (
              <p className="my-2" key={elementId}>
                {headline(elementId)}
              </p>
            ))}
          </div>
        </div>
      )}
      {status === "aborted" && (
        <div className="flex">
          <div
            className="flex w-0.5 grow items-start justify-center"
            style={{
              background:
                "repeating-linear-gradient(to bottom, rgb(148 163 184), rgb(148 163 184) 2px, transparent 2px, transparent 10px)",
            }}>
            <XCircleIcon className="min-h-6 min-w-6 rounded-full bg-white text-slate-400" />
          </div>
          <div className="mb-2 ml-4 flex flex-col">
            <p
              data-testid="tooltip-survey_closed"
              className="mb-2 w-fit rounded-lg bg-slate-100 px-2 font-medium text-slate-700">
              {t("workspace.surveys.responses.survey_closed")}
            </p>
            {skippedElements.map((elementId) => (
              <p className="my-2" key={elementId}>
                {headline(elementId)}
              </p>
            ))}
          </div>
        </div>
      )}
    </div>
  );
};
