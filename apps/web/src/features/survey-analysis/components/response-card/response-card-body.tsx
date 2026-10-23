import { getComputedEmbeddedFields, getIngestedEmbeddedFields } from "@forma/types/embedded-data-resolver";
import type { TResponseWithQuotas } from "@forma/types/responses";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type { TSurvey } from "@forma/types/surveys/types";
import { getTextContent } from "@forma/types/surveys/validation";
import { CheckCircle2Icon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { getLocalizedValue } from "../../../../lib/survey/i18n";
import { parseRecallInfo } from "../../../../lib/survey/recall";
import { getElementsFromBlocks } from "../../../survey-editor/lib/client-utils";
import { isValidValue, splitRecallHighlights } from "../../lib/response-card";
import { buildServerEmbeddedValues, getSurveyDateFormatMap } from "../../lib/response-display";
import { HiddenFields, ResponseCardQuotas, ResponseVariables, VerifiedEmail } from "./card-fields";
import { ElementSkip } from "./element-skip";
import { RenderResponse } from "./render-response";

/** A headline with its recalled values marked, as forma2 drew them: the parts between #/ and \# are the recalled ones. */
const formatRecallHighlights = (text: string) =>
  splitRecallHighlights(text).map((part, index) =>
    index % 2 === 1 ? (
      // The split alternates plain and recalled parts; the position is the part's identity.
      // oxlint-disable-next-line react/no-array-index-key -- see above
      <span
        key={index}
        className="mr-0.5 ml-0.5 rounded-md border border-slate-200 bg-slate-50 px-1 py-0.5 text-sm first:ml-0">
        @{part}
      </span>
    ) : (
      part
    )
  );

/** forma2's SingleResponseCardBody: every element's answer or skip, the fields, the quotas and the completion mark. */
export const ResponseCardBody = ({
  survey,
  response,
  skippedQuestions,
  locale,
}: {
  survey: TSurvey;
  response: TResponseWithQuotas;
  skippedQuestions: string[][];
  locale: string;
}) => {
  const { t } = useTranslation();
  const elements = getElementsFromBlocks(survey.blocks);
  const computedFields = getComputedEmbeddedFields(survey);
  const ingestedFields = getIngestedEmbeddedFields(survey);
  const dateFormats = getSurveyDateFormatMap(elements);
  const recallValues = buildServerEmbeddedValues(response, survey);
  const isFirstElementAnswered = elements[0] ? Boolean(response.data[elements[0].id]) : false;
  let remainingSkips = skippedQuestions;

  return (
    <div className="p-6">
      {survey.welcomeCard.enabled && (
        <ElementSkip
          skippedElements={[]}
          elements={elements}
          status="welcomeCard"
          isFirstElementAnswered={isFirstElementAnswered}
          recallValues={recallValues}
          variables={response.variables}
          locale={locale}
        />
      )}
      <div className="space-y-6">
        {survey.isVerifyEmailEnabled && response.data.verifiedEmail && (
          <VerifiedEmail responseData={response.data} />
        )}
        {elements.map((element) => {
          // A call to action without an external button is skipped unless it holds an answer from when it had one.
          if (
            element.type === TSurveyElementTypeEnum.CTA &&
            !element.buttonExternal &&
            !response.data[element.id]
          ) {
            return null;
          }
          const skipped = remainingSkips.find((group) => group.includes(element.id));
          if (skipped) remainingSkips = remainingSkips.filter((group) => group !== skipped);

          return (
            <div key={element.id}>
              {isValidValue(response.data[element.id]) ? (
                <div>
                  <p className="mb-1 text-sm font-semibold text-slate-600">
                    {formatRecallHighlights(
                      getTextContent(
                        parseRecallInfo(
                          getLocalizedValue(element.headline, "default"),
                          recallValues,
                          response.variables,
                          true,
                          locale,
                          dateFormats
                        )
                      )
                    )}
                  </p>
                  <div dir="auto">
                    <RenderResponse
                      element={element}
                      survey={survey}
                      responseData={response.data[element.id]}
                      language={response.language}
                      locale={locale}
                      showId
                    />
                  </div>
                </div>
              ) : (
                <ElementSkip
                  skippedElements={skipped}
                  elements={elements}
                  recallValues={recallValues}
                  variables={response.variables}
                  locale={locale}
                  status={
                    response.finished ||
                    (remainingSkips.length > 0 &&
                      !remainingSkips[remainingSkips.length - 1]?.includes(element.id))
                      ? "skipped"
                      : "aborted"
                  }
                />
              )}
            </div>
          );
        })}
      </div>
      {computedFields.length > 0 && (
        <ResponseVariables variables={computedFields} variablesData={response.variables} />
      )}
      {ingestedFields.length > 0 && (
        <HiddenFields hiddenFields={ingestedFields} responseData={response.data} />
      )}
      <ResponseCardQuotas quotas={response.quotas} />
      {response.finished && (
        <div className="mt-4 flex items-center">
          <CheckCircle2Icon className="size-6 text-slate-400" />
          <p className="mx-2 rounded-lg bg-slate-100 px-2 text-sm font-medium text-slate-700">
            {t("common.completed")}
          </p>
        </div>
      )}
    </div>
  );
};
