import { useQuery } from "@tanstack/react-query";
import { EyeOffIcon, FilterIcon, LinkIcon, TriangleAlertIcon } from "lucide-react";
import { type ReactNode, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Wordmark } from "../../../../components/ui/logo";
import { ApiError } from "../../../../lib/api";
import { errorMessage } from "../../../../lib/errors";
import { replaceHeadlineRecall } from "../../../../lib/survey/recall";
import { RESULT_SHARE_NOT_FOUND, sharedResultsQuery } from "../../result-shares";
import { ResponseFilterProvider } from "../filter/response-filter-context";
import { ElementImpressionsProvider } from "../summary/element-impressions-context";
import { SummaryList } from "../summary/summary-list";
import { SummaryMetadata, type SummaryTab } from "../summary/summary-metadata";
import { SharedSummaryContext } from "../summary/summary-parts";
import { QuotasSummary, SummaryDropOffs } from "../summary/summary-tabs";
import { TrendsCard } from "../summary/trends-card";

/** The frame of the page a results link opens: the brand, then the page's content, readable without an account. */
const SharedResultsFrame = ({ children }: { children: ReactNode }) => (
  <div className="min-h-screen bg-slate-50">
    <header className="border-b border-slate-200 bg-white">
      <div className="mx-auto flex max-w-6xl items-center px-4 py-3 md:px-6">
        <Wordmark className="text-2xl" />
      </div>
    </header>
    <main className="mx-auto max-w-6xl space-y-6 px-4 py-6 md:px-6">{children}</main>
  </div>
);

/** What a key that opens nothing shows: the link is unknown, expired or revoked, which the server does not tell apart. */
const SharedResultsUnavailable = ({ message }: { message: string }) => {
  const { t } = useTranslation();
  return (
    <SharedResultsFrame>
      <div className="mx-auto mt-16 max-w-md rounded-xl border border-slate-200 bg-white p-8 text-center shadow-xs">
        <LinkIcon className="mx-auto mb-4 size-8 text-slate-400" aria-hidden="true" />
        <h1 className="text-lg font-semibold text-slate-900">
          {t("workspace.analysis.result_shares.unavailable_title")}
        </h1>
        <p className="mt-2 text-sm text-slate-600">{message}</p>
      </div>
    </SharedResultsFrame>
  );
};

/**
 * The summary a read-only results link shows to people without an account: the metrics, drop-offs and quotas, the trend and the element cards, under the filter the link froze. Nothing on it filters, opens a response or names a respondent (SharedSummaryContext), and the cards the link withholds show only their counts.
 */
export const SharedResultsPage = ({ token }: { token: string | undefined }) => {
  const { t, i18n } = useTranslation();
  const query = useQuery({ ...sharedResultsQuery(token ?? ""), enabled: token !== undefined });
  const [tab, setTab] = useState<SummaryTab | undefined>(undefined);
  const survey = useMemo(
    () => (query.data ? replaceHeadlineRecall(query.data.survey, "default") : undefined),
    [query.data]
  );
  const locale = i18n.resolvedLanguage ?? i18n.language;

  if (token === undefined) {
    return (
      <SharedResultsUnavailable message={t("workspace.analysis.result_shares.unavailable_description")} />
    );
  }
  if (query.isPending) {
    return (
      <SharedResultsFrame>
        <div aria-busy="true" className="h-64 animate-pulse rounded-xl bg-slate-200" />
      </SharedResultsFrame>
    );
  }
  if (query.isError || !survey) {
    const notFound = query.error instanceof ApiError && query.error.code === RESULT_SHARE_NOT_FOUND;
    return (
      <SharedResultsUnavailable
        message={
          notFound
            ? t("workspace.analysis.result_shares.unavailable_description")
            : errorMessage(t, query.error)
        }
      />
    );
  }
  const { link, summary, withheldIds } = query.data;
  const formatDate = new Intl.DateTimeFormat(locale, { dateStyle: "long" });

  return (
    <SharedResultsFrame>
      <SharedSummaryContext.Provider value={true}>
        <ResponseFilterProvider survey={survey}>
          <div className="space-y-2">
            <p className="text-sm font-medium text-slate-500">
              {t("workspace.analysis.result_shares.shared_results")}
            </p>
            <h1 className="text-2xl font-bold text-slate-900">{survey.name}</h1>
            {link.name !== "" && <p className="text-slate-600">{link.name}</p>}
            <div className="flex flex-wrap gap-2 text-xs text-slate-600">
              {link.filtered && (
                <span className="inline-flex items-center gap-1.5 rounded-lg bg-slate-100 px-2 py-1">
                  <FilterIcon className="size-3.5" aria-hidden="true" />
                  {t("workspace.analysis.result_shares.filtered_view")}
                </span>
              )}
              {link.segmentMissing && (
                <span className="inline-flex items-center gap-1.5 rounded-lg bg-amber-50 px-2 py-1 text-amber-800">
                  <TriangleAlertIcon className="size-3.5" aria-hidden="true" />
                  {t("workspace.analysis.result_shares.segment_missing_notice")}
                </span>
              )}
              {withheldIds.size > 0 && (
                <span className="inline-flex items-center gap-1.5 rounded-lg bg-slate-100 px-2 py-1">
                  <EyeOffIcon className="size-3.5" aria-hidden="true" />
                  {t("workspace.analysis.result_shares.withheld_notice")}
                </span>
              )}
              {link.expiresAt && (
                <span className="rounded-lg bg-slate-100 px-2 py-1">
                  {t("workspace.analysis.result_shares.available_until", {
                    date: formatDate.format(new Date(link.expiresAt)),
                  })}
                </span>
              )}
            </div>
          </div>
          <SummaryMetadata
            surveySummary={summary.meta}
            quotasCount={summary.quotas.length}
            isLoading={false}
            tab={tab}
            setTab={setTab}
          />
          {tab === "dropOffs" && <SummaryDropOffs dropOff={summary.dropOff} survey={survey} />}
          {tab === "quotas" && <QuotasSummary quotas={summary.quotas} />}
          <TrendsCard survey={survey} locale={locale} sharedToken={token} />
          <ElementImpressionsProvider dropOff={summary.dropOff}>
            <SummaryList
              summary={summary.summary}
              responseCount={summary.meta.totalResponses}
              survey={survey}
              appSetupCompleted
              locale={locale}
              isReadOnly
              withheldIds={withheldIds}
            />
          </ElementImpressionsProvider>
        </ResponseFilterProvider>
      </SharedSummaryContext.Provider>
    </SharedResultsFrame>
  );
};
