import type { TResponseDataValue } from "@forma/types/responses";
import { type TSurveyElement, TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type { TSurvey } from "@forma/types/surveys/types";
import { CheckCheckIcon, MousePointerClickIcon, PhoneIcon } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "../../../../lib/cn";
import { formatStoredDateForDisplay } from "../../../../lib/survey/date-display";
import { getLocalizedValue } from "../../../../lib/survey/i18n";
import { getLanguageCode, renderHyperlinkedContent } from "../../lib/response-display";
import { getChoiceIdByValue, processResponseData } from "../../lib/responses";
import { RatingResponse } from "../response-values/rating-response";
import {
  ArrayResponse,
  FileUploadResponse,
  PictureSelectionResponse,
  RankingResponse,
  ResponseBadges,
} from "../response-values/response-values";

const isEmptyAnswer = (responseData: TResponseDataValue) =>
  (typeof responseData === "string" && responseData === "") ||
  (Array.isArray(responseData) && responseData.length === 0) ||
  (typeof responseData === "object" && responseData !== null && Object.keys(responseData).length === 0);

/** forma2's RenderResponse: one answer drawn the way its element type reads best. */
export const RenderResponse = ({
  responseData,
  element,
  survey,
  language,
  locale,
  isExpanded = true,
  showId,
}: {
  responseData: TResponseDataValue;
  element: TSurveyElement;
  survey: TSurvey;
  language: string | null;
  locale: string;
  isExpanded?: boolean;
  showId: boolean;
}): ReactNode => {
  if (isEmptyAnswer(responseData)) {
    return <p className="ph-no-capture my-1 font-normal text-slate-700">-</p>;
  }

  switch (element.type) {
    case TSurveyElementTypeEnum.Rating:
    case TSurveyElementTypeEnum.CSAT:
    case TSurveyElementTypeEnum.CES:
      if (typeof responseData === "number") {
        return (
          <RatingResponse
            scale={element.scale}
            answer={responseData}
            range={element.range}
            addColors={element.isColorCodingEnabled}
          />
        );
      }
      break;
    case TSurveyElementTypeEnum.Date:
      if (typeof responseData === "string") {
        const formattedDate =
          formatStoredDateForDisplay(responseData, element.format, locale) ?? responseData;
        return <p className="ph-no-capture my-1 truncate font-normal text-slate-700">{formattedDate}</p>;
      }
      break;
    case TSurveyElementTypeEnum.PictureSelection:
      if (Array.isArray(responseData)) {
        return (
          <PictureSelectionResponse
            choices={element.choices}
            selected={responseData}
            isExpanded={isExpanded}
            showId={showId}
          />
        );
      }
      break;
    case TSurveyElementTypeEnum.FileUpload:
      if (Array.isArray(responseData)) return <FileUploadResponse selected={responseData} />;
      break;
    case TSurveyElementTypeEnum.Matrix:
      if (typeof responseData === "object" && !Array.isArray(responseData)) {
        const languageCode = getLanguageCode(survey.languages, language);
        return (
          <>
            {element.rows.map((row) => {
              const rowLabel = getLocalizedValue(row.label, languageCode);
              const answer = responseData[rowLabel];
              if (!answer) return null;
              return (
                <p key={rowLabel} className="ph-no-capture my-1 font-normal text-slate-700">
                  {rowLabel}:{processResponseData(answer)}
                </p>
              );
            })}
          </>
        );
      }
      break;
    case TSurveyElementTypeEnum.Address:
    case TSurveyElementTypeEnum.ContactInfo:
      if (Array.isArray(responseData)) return <ArrayResponse value={responseData} />;
      break;
    case TSurveyElementTypeEnum.Cal:
    case TSurveyElementTypeEnum.Consent:
    case TSurveyElementTypeEnum.CTA:
      if (typeof responseData === "string" || typeof responseData === "number") {
        const Icon =
          element.type === TSurveyElementTypeEnum.Cal
            ? PhoneIcon
            : element.type === TSurveyElementTypeEnum.Consent
              ? CheckCheckIcon
              : MousePointerClickIcon;
        return (
          <ResponseBadges
            items={[{ value: responseData.toString() }]}
            isExpanded={isExpanded}
            icon={<Icon className="size-4 text-slate-500" />}
            showId={showId}
          />
        );
      }
      break;
    case TSurveyElementTypeEnum.MultipleChoiceMulti:
    case TSurveyElementTypeEnum.MultipleChoiceSingle:
    case TSurveyElementTypeEnum.Ranking:
      if (typeof responseData === "string" || typeof responseData === "number") {
        return (
          <ResponseBadges
            items={[
              { value: responseData.toString(), id: getChoiceIdByValue(responseData.toString(), element) },
            ]}
            isExpanded={isExpanded}
            showId={showId}
          />
        );
      }
      if (Array.isArray(responseData)) {
        const items = responseData
          .filter((choice) => choice !== "")
          .map((choice) => ({ value: choice, id: getChoiceIdByValue(choice, element) }));
        return element.type === TSurveyElementTypeEnum.Ranking ? (
          <RankingResponse value={items} isExpanded={isExpanded} showId={showId} />
        ) : (
          <ResponseBadges items={items} isExpanded={isExpanded} showId={showId} />
        );
      }
      break;
    default:
      if (
        typeof responseData === "string" ||
        typeof responseData === "number" ||
        Array.isArray(responseData)
      ) {
        return (
          <p
            className={cn(
              "ph-no-capture my-1 truncate font-normal text-slate-700",
              isExpanded ? "whitespace-pre-line" : "whitespace-nowrap"
            )}>
            {typeof responseData === "string"
              ? renderHyperlinkedContent(responseData)
              : Array.isArray(responseData)
                ? responseData.join(", ")
                : responseData}
          </p>
        );
      }
  }
  return null;
};
