import type { TSurvey } from "@forma/types/surveys/types";
import { hashKey, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { RefreshCwIcon, SparklesIcon } from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Badge } from "../../../../components/ui/badge";
import { Button } from "../../../../components/ui/button";
import { ProgressBar } from "../../../../components/ui/progress-bar";
import { ApiError } from "../../../../lib/api";
import { cn } from "../../../../lib/cn";
import { errorMessage } from "../../../../lib/errors";
import { APP_LANGUAGES } from "../../../../lib/languages";
import { timeSince } from "../../../../lib/time";
import { getAIUnavailableMessage, getAIUnavailableMessageForErrorCode } from "../../../ai/availability";
import {
  type TextSentimentLabel,
  type TextSummary,
  type TextSummaryRequest,
  type TextSummaryTarget,
  analysisApi,
  isTextSummaryOverdue,
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

/** Whether an open-text question asks for text, the only kind whose answers are summarized: not an email address, a phone number, a URL or a number. A question saved without an input type asks for text. */
export const isFreeTextInput = (inputType: string | undefined): boolean =>
  inputType === undefined || inputType === "text";

/** The AI summary of a question's free-text answers under the current filter: the themes the answers share, each with its share and quotes that open their responses, and the overall sentiment. Nothing is generated until the reader asks; the generation runs in the background and the card follows it until the summary lands. A summary stays until its answers change, and is shown to readers of other languages, labelled with its own. A read-only results link shows none. */
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
  const state = useQuery(textSummaryQuery(survey.id, request));
  // The request travels with the mutation: a pending mutation takes the options of the latest render, so a summary must be stored under the filter and language it was asked for, not the ones the reader has switched to since. A queued generation keeps the summary on screen next to it (the one it refreshes, or one in another language) until the lookup, which polls while it is pending, brings the new one.
  const generate = useMutation({
    mutationFn: (vars: { request: TextSummaryRequest; refresh: boolean }) =>
      analysisApi.generateTextSummary(survey.id, vars.request, vars.refresh),
    onSuccess: (next, vars) =>
      queryClient.setQueryData(textSummaryQuery(survey.id, vars.request).queryKey, (previous) =>
        next.summary === null && previous?.summary ? { ...next, summary: previous.summary } : next
      ),
  });
  const run = (refresh: boolean) => generate.mutate({ request, refresh });
  // A generation pending or failed for another filter or language is not this one's.
  const isCurrent =
    generate.variables !== undefined && hashKey([generate.variables.request]) === hashKey([request]);
  const [openResponseId, setOpenResponseId] = useState<string | null>(null);

  if (state.isPending) return null;
  if (state.isError) {
    return (
      <div className="px-4 pb-4 text-sm text-red-600 md:px-6" role="alert">
        {errorMessage(t, state.error)}
      </div>
    );
  }
  const { available, reason, summary, status, error } = state.data;
  // An open-text question that asks for an email address, a phone number, a URL or a number has nothing to summarize.
  if (reason === "not_free_text") return null;

  const overdue = isTextSummaryOverdue(state.data);
  const pending = (generate.isPending && isCurrent) || (status === "pending" && !overdue);
  // A summary in another language than the reader's is labelled with it; a member who can generate one is offered one in their own language instead of a refresh.
  const inOtherLanguage = summary !== null && summary.language !== locale;
  const failure =
    generate.isError && isCurrent
      ? generationError(generate.error, t)
      : status === "failed" && available && !pending
        ? failedGeneration(error?.code, t)
        : null;
  const summarizeLabel =
    target === "other"
      ? t("workspace.surveys.summary.ai_summary.summarize_other")
      : t("workspace.surveys.summary.ai_summary.summarize");
  const progress = pending ? (
    <p role="status" className="text-sm text-slate-600">
      {t("workspace.surveys.summary.ai_summary.summarizing")}
    </p>
  ) : overdue ? (
    <p role="status" className="text-sm text-slate-600">
      {t("workspace.surveys.summary.ai_summary.taking_long")}
    </p>
  ) : null;

  return (
    <section
      aria-label={t("workspace.surveys.summary.ai_summary.title")}
      className={cn("rounded-lg border border-slate-200 bg-slate-50 p-4", className)}>
      {summary ? (
        <>
          {/* A member without edit access reads the summary others generated, and cannot refresh it. */}
          <SummaryView
            summary={summary}
            locale={locale}
            refreshing={pending}
            onRefresh={available && !inOtherLanguage ? () => run(true) : undefined}
            onOpenResponse={setOpenResponseId}
          />
          {inOtherLanguage && (
            <div className="mt-4 flex flex-wrap items-center gap-3">
              <p className="text-sm text-slate-600">
                {t("workspace.surveys.summary.ai_summary.other_language", {
                  language: languageName(summary.language),
                })}
              </p>
              {available && (
                <Button variant="secondary" size="sm" loading={pending} onClick={() => run(false)}>
                  <SparklesIcon />
                  {t("workspace.surveys.summary.ai_summary.summarize_in_language", {
                    language: languageName(locale),
                  })}
                </Button>
              )}
              {progress}
            </div>
          )}
          {!inOtherLanguage && overdue && <div className="mt-3">{progress}</div>}
        </>
      ) : reason === "read_only" ? (
        // Not a permission error: there is simply no summary yet, and someone with edit access can make one.
        (progress ?? (
          <p className="text-sm text-slate-600">{t("workspace.surveys.summary.ai_summary.read_only_none")}</p>
        ))
      ) : !available ? (
        <div className="flex flex-wrap items-center gap-3">
          <Button variant="secondary" size="sm" disabled>
            <SparklesIcon />
            {summarizeLabel}
          </Button>
          <p className="text-sm text-slate-600">{getAIUnavailableMessage(reason, t)}</p>
        </div>
      ) : (
        <div className="flex flex-wrap items-center gap-3">
          <Button variant="secondary" size="sm" disabled={pending} onClick={() => run(false)}>
            <SparklesIcon />
            {summarizeLabel}
          </Button>
          {progress}
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

/** A language's own name when the app offers it ("日本語"), else its tag. */
const languageName = (code: string): string =>
  APP_LANGUAGES.find((language) => language.code === code)?.native ?? code;

/** What a refused request says: AI switched off or unconfigured since the page loaded, the provider rate limiting the instance, or the usual error wording. */
const generationError = (error: unknown, t: ReturnType<typeof useTranslation>["t"]): string => {
  if (error instanceof ApiError) {
    const unavailable = getAIUnavailableMessageForErrorCode(error.code, t);
    if (unavailable) return unavailable;
    if (error.code === "ai_quota_exceeded") return t("workspace.surveys.summary.ai_summary.quota_exceeded");
  }
  return errorMessage(t, error);
};

/** What a generation that failed in the worker says, by the code it ended with: AI switched off or unconfigured since it was asked for, the provider rate limiting the instance, or that it can be asked for again. */
const failedGeneration = (code: string | undefined, t: ReturnType<typeof useTranslation>["t"]): string => {
  const unavailable = getAIUnavailableMessageForErrorCode(code, t);
  if (unavailable) return unavailable;
  if (code === "ai_quota_exceeded") return t("workspace.surveys.summary.ai_summary.quota_exceeded");
  return t("workspace.surveys.summary.ai_summary.failed");
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
  /** Absent when the reader cannot generate a summary. */
  onRefresh?: () => void;
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
          {onRefresh && (
            <Button variant="ghost" size="sm" loading={refreshing} onClick={onRefresh}>
              <RefreshCwIcon />
              {t("workspace.surveys.summary.ai_summary.refresh")}
            </Button>
          )}
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
