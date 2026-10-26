import type { TSurvey } from "@forma/types/surveys/types";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { RefreshCwIcon, SparklesIcon } from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Badge } from "../../../../components/ui/badge";
import { Button } from "../../../../components/ui/button";
import { ProgressBar } from "../../../../components/ui/progress-bar";
import { ApiError } from "../../../../lib/api";
import { cn } from "../../../../lib/cn";
import { errorMessage } from "../../../../lib/errors";
import { timeSince } from "../../../../lib/time";
import { getAIUnavailableMessage, getAIUnavailableMessageForErrorCode } from "../../../ai/availability";
import {
  type TextSentimentLabel,
  type TextSummary,
  type TextSummaryRequest,
  type TextSummaryTarget,
  analysisApi,
  textSummaryQuery,
} from "../../api";
import { useResponseFilter } from "../filter/response-filter-context";
import { ResponseSampleModal } from "./response-sample-modal";
import { useIsSharedSummary } from "./summary-parts";

const SENTIMENT_BADGE: Record<TextSentimentLabel, "success" | "gray" | "error" | "warning"> = {
  positive: "success",
  neutral: "gray",
  negative: "error",
  mixed: "warning",
};

/** The AI summary of a question's free-text answers under the current filter: the themes the answers share, each with its share and quotes that open their responses, and the overall sentiment. Nothing is generated until the reader asks; a summary stays until its answers change. A read-only results link shows none. */
export const TextSummaryPanel = (props: {
  survey: TSurvey;
  elementId: string;
  target: TextSummaryTarget;
  answerCount: number;
  locale: string;
  isReadOnly: boolean;
  /** The panel's margins in the card that holds it. */
  className?: string;
}) => {
  const isShared = useIsSharedSummary();
  if (isShared || props.answerCount === 0) return null;
  return <TextSummarySection {...props} />;
};

const TextSummarySection = ({
  survey,
  elementId,
  target,
  locale,
  isReadOnly,
  className = "mx-4 mb-5 md:mx-6",
}: {
  survey: TSurvey;
  elementId: string;
  target: TextSummaryTarget;
  locale: string;
  isReadOnly: boolean;
  className?: string;
}) => {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { filterCriteria } = useResponseFilter();
  const request = useMemo<TextSummaryRequest>(
    () => ({ elementId, target, language: locale, filterCriteria }),
    [elementId, target, locale, filterCriteria]
  );
  const query = textSummaryQuery(survey.id, request);
  const state = useQuery(query);
  const generate = useMutation({
    mutationFn: (refresh: boolean) => analysisApi.generateTextSummary(survey.id, request, refresh),
    onSuccess: (next) => queryClient.setQueryData(query.queryKey, next),
  });
  const [openResponseId, setOpenResponseId] = useState<string | null>(null);

  if (state.isPending) return null;
  if (state.isError) {
    return (
      <div className="px-4 pb-4 text-sm text-red-600 md:px-6" role="alert">
        {errorMessage(t, state.error)}
      </div>
    );
  }
  const { available, reason, summary } = state.data;

  const failure = generate.isError ? generationError(generate.error, t) : null;
  const summarizeLabel =
    target === "other"
      ? t("workspace.surveys.summary.ai_summary.summarize_other")
      : t("workspace.surveys.summary.ai_summary.summarize");

  return (
    <section
      aria-label={t("workspace.surveys.summary.ai_summary.title")}
      className={cn("rounded-lg border border-slate-200 bg-slate-50 p-4", className)}>
      {!available ? (
        <div className="flex flex-wrap items-center gap-3">
          <Button variant="secondary" size="sm" disabled>
            <SparklesIcon />
            {summarizeLabel}
          </Button>
          <p className="text-sm text-slate-600">{getAIUnavailableMessage(reason, t)}</p>
        </div>
      ) : summary ? (
        <SummaryView
          summary={summary}
          locale={locale}
          refreshing={generate.isPending}
          onRefresh={() => generate.mutate(true)}
          onOpenResponse={setOpenResponseId}
        />
      ) : (
        <div className="flex flex-wrap items-center gap-3">
          <Button
            variant="secondary"
            size="sm"
            disabled={generate.isPending}
            onClick={() => generate.mutate(false)}>
            <SparklesIcon />
            {summarizeLabel}
          </Button>
          {generate.isPending && (
            <p role="status" className="text-sm text-slate-600">
              {t("workspace.surveys.summary.ai_summary.summarizing")}
            </p>
          )}
        </div>
      )}
      {failure && (
        <p role="alert" className="mt-3 text-sm text-red-600">
          {failure}
        </p>
      )}
      {available && (
        <p className="mt-3 text-xs text-slate-500">
          {t("workspace.surveys.summary.ai_summary.privacy_note")}
        </p>
      )}
      <ResponseSampleModal
        responseId={openResponseId}
        onClose={() => setOpenResponseId(null)}
        survey={survey}
        isReadOnly={isReadOnly}
        locale={locale}
      />
    </section>
  );
};

/** What a failed generation says: AI switched off or unconfigured since the page loaded, the provider rate limiting the instance, or the usual error wording. */
const generationError = (error: unknown, t: ReturnType<typeof useTranslation>["t"]): string => {
  if (error instanceof ApiError) {
    const unavailable = getAIUnavailableMessageForErrorCode(error.code, t);
    if (unavailable) return unavailable;
    if (error.code === "ai_quota_exceeded") return t("workspace.surveys.summary.ai_summary.quota_exceeded");
  }
  return errorMessage(t, error);
};

const SummaryView = ({
  summary,
  locale,
  refreshing,
  onRefresh,
  onOpenResponse,
}: {
  summary: TextSummary;
  locale: string;
  refreshing: boolean;
  onRefresh: () => void;
  onOpenResponse: (responseId: string) => void;
}) => {
  const { t } = useTranslation();
  const { sentiment } = summary;
  const sentimentLabels: Record<TextSentimentLabel, string> = {
    positive: t("workspace.surveys.summary.ai_summary.sentiment_positive"),
    neutral: t("workspace.surveys.summary.ai_summary.sentiment_neutral"),
    negative: t("workspace.surveys.summary.ai_summary.sentiment_negative"),
    mixed: t("workspace.surveys.summary.ai_summary.sentiment_mixed"),
  };
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <h4 className="flex items-center gap-1.5 text-sm font-semibold text-slate-900">
            <SparklesIcon className="size-4 text-slate-500" aria-hidden />
            {t("workspace.surveys.summary.ai_summary.title")}
          </h4>
          <Badge
            type={SENTIMENT_BADGE[sentiment.overall]}
            text={t("workspace.surveys.summary.ai_summary.sentiment", {
              sentiment: sentimentLabels[sentiment.overall],
            })}
            title={t("workspace.surveys.summary.ai_summary.sentiment_shares", {
              positive: sentiment.positive,
              neutral: sentiment.neutral,
              negative: sentiment.negative,
            })}
          />
        </div>
        <div className="flex items-center gap-2 text-xs text-slate-500">
          <span>
            {t("workspace.surveys.summary.ai_summary.generated_ago", {
              time: timeSince(summary.generatedAt, locale),
            })}
          </span>
          <Button variant="ghost" size="sm" loading={refreshing} onClick={onRefresh}>
            <RefreshCwIcon />
            {t("workspace.surveys.summary.ai_summary.refresh")}
          </Button>
        </div>
      </div>
      <p className="text-xs text-slate-600">
        {t("workspace.surveys.summary.ai_summary.sentiment_shares", {
          positive: sentiment.positive,
          neutral: sentiment.neutral,
          negative: sentiment.negative,
        })}
        {summary.sampledCount < summary.answerCount && (
          <>
            {" "}
            {t("workspace.surveys.summary.ai_summary.sampled", {
              sampled: summary.sampledCount,
              total: summary.answerCount,
            })}
          </>
        )}
        {summary.readLimit > 0 && (
          <> {t("workspace.surveys.summary.ai_summary.read_limited", { count: summary.readLimit })}</>
        )}
      </p>
      {summary.themes.length === 0 ? (
        <p className="text-sm text-slate-600">{t("workspace.surveys.summary.ai_summary.no_themes")}</p>
      ) : (
        <ol className="space-y-4">
          {summary.themes.map((theme, index) => (
            // A summary is never reordered and two themes may share a title; the position tells them apart.
            // oxlint-disable-next-line react/no-array-index-key -- see above
            <li key={index} className="space-y-2">
              <div className="flex flex-wrap items-baseline justify-between gap-2">
                <p className="font-semibold text-slate-900">{theme.title}</p>
                <p className="text-xs text-slate-600">
                  {t("workspace.surveys.summary.ai_summary.theme_share", {
                    percentage: theme.percentage,
                    count: theme.count,
                  })}
                </p>
              </div>
              <ProgressBar progress={theme.percentage / 100} barColor="bg-slate-700" height={2} />
              <p className="text-sm text-slate-700">{theme.description}</p>
              <ul className="space-y-1">
                {theme.quotes.map((quote) => (
                  <li key={quote.responseId + quote.text}>
                    <button
                      type="button"
                      className="ph-no-capture focus-visible:ring-ring w-full cursor-pointer rounded-md border-l-2 border-slate-300 bg-white px-3 py-1.5 text-left text-sm whitespace-pre-wrap text-slate-700 italic hover:border-slate-500 hover:bg-slate-100 focus-visible:ring-2 focus-visible:outline-hidden"
                      title={t("workspace.surveys.summary.open_response_details")}
                      onClick={() => onOpenResponse(quote.responseId)}>
                      “{quote.text}”
                    </button>
                  </li>
                ))}
              </ul>
            </li>
          ))}
        </ol>
      )}
    </div>
  );
};
