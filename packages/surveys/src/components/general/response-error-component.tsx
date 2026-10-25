import { useTranslation } from "react-i18next";
import { type TResponseData } from "@forma/types/responses";
import { type TSurveyElement } from "@forma/types/surveys/elements";
import { SubmitButton } from "@/components/buttons/submit-button";
import { processResponseData } from "@/lib/response";

interface ResponseErrorComponentProps {
  readonly questions: TSurveyElement[];
  readonly responseData: TResponseData;
  readonly onRetry?: () => void;
  readonly isRetrying?: boolean;
}

export function ResponseErrorComponent({
  questions,
  responseData,
  onRetry,
  isRetrying = false,
}: ResponseErrorComponentProps) {
  const { t } = useTranslation();
  return (
    <div className="bg-survey-bg text-heading flex flex-col p-4" role="alert" aria-live="assertive">
      <span className="mb-1.5 text-base leading-6 font-bold">{t("common.your_feedback_is_stuck")}</span>
      <p className="max-w-md text-sm leading-6 font-normal">
        {t("common.the_servers_cannot_be_reached_at_the_moment")}
        <br />
        {t("common.please_retry_now_or_try_again_later")}
      </p>
      <div className="mt-4 rounded-lg border border-slate-200 bg-slate-100 px-4 py-5">
        <div className="flex max-h-48 flex-1 flex-col space-y-2 overflow-y-scroll">
          {questions.map((question, index) => {
            const response = responseData[question.id];
            if (!response) return;
            return (
              <div className="flex flex-col" key={`response-${index.toString()}`}>
                <span className="text-sm leading-5 text-slate-900">{`${t("common.question")} ${(index + 1).toString()}`}</span>
                <span className="text-sm leading-5 font-semibold text-slate-900">
                  {processResponseData(response)}
                </span>
              </div>
            );
          })}
        </div>
      </div>
      <div className="mt-4 flex flex-1 flex-row items-center justify-end space-x-2">
        <SubmitButton
          buttonLabel={isRetrying ? t("common.retrying") : t("common.retry")}
          isLastQuestion={false}
          onClick={() => {
            if (!isRetrying) {
              onRetry?.();
            }
          }}
          disabled={isRetrying}
        />
      </div>
    </div>
  );
}

interface ResponseSendingDelayedNoticeProps {
  readonly isOffline: boolean;
}

/**
 * Shown above the current card while an answer is waiting to be sent. It does not block the survey: the respondent keeps answering and the queue sends everything once the connection is back. A status, not an alert, so a screen reader announces it without interrupting.
 */
export function ResponseSendingDelayedNotice({ isOffline }: ResponseSendingDelayedNoticeProps) {
  const { t } = useTranslation();
  return (
    <div className="px-4 pt-2" role="status" aria-live="polite">
      <div className="flex w-full items-center gap-2 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2">
        <svg
          xmlns="http://www.w3.org/2000/svg"
          viewBox="0 0 20 20"
          fill="currentColor"
          aria-hidden="true"
          className="h-4 w-4 shrink-0 text-amber-500">
          <path
            fillRule="evenodd"
            d="M8.485 2.495c.673-1.167 2.357-1.167 3.03 0l6.28 10.875c.673 1.167-.17 2.625-1.516 2.625H3.72c-1.345 0-2.189-1.458-1.515-2.625L8.485 2.495zM10 6a.75.75 0 01.75.75v3.5a.75.75 0 01-1.5 0v-3.5A.75.75 0 0110 6zm0 9a1 1 0 100-2 1 1 0 000 2z"
            clipRule="evenodd"
          />
        </svg>
        <p className="text-xs text-amber-700">
          {isOffline ? t("common.saving_offline") : t("common.saving_reconnecting")}
        </p>
      </div>
    </div>
  );
}
