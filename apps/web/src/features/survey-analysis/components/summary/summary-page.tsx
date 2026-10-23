import type { TSurvey, TSurveySummary } from "@forma/types/surveys/types";
import { keepPreviousData, useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { errorMessage } from "../../../../lib/errors";
import { replaceHeadlineRecall } from "../../../../lib/survey/recall";
import { surveyDisplaysQuery, surveySummaryQuery } from "../../api";
import { CustomFilter } from "../filter/custom-filter";
import { useResponseFilter } from "../filter/response-filter-context";
import { ElementImpressionsProvider } from "./element-impressions-context";
import { ScrollToTop } from "./scroll-to-top";
import { SummaryList } from "./summary-list";
import { SummaryMetadata, type SummaryTab } from "./summary-metadata";
import { QuotasSummary, SummaryDropOffs, SummaryImpressions } from "./summary-tabs";

const EMPTY_SUMMARY: TSurveySummary = {
  meta: {
    completedPercentage: 0,
    completedResponses: 0,
    displayCount: 0,
    dropOffPercentage: 0,
    dropOffCount: 0,
    startsPercentage: 0,
    totalResponses: 0,
    ttcAverage: 0,
    quotasCompleted: 0,
    quotasCompletedPercentage: 0,
  },
  dropOff: [],
  quotas: [],
  summary: [],
};

/** forma2's SummaryPage: the metrics row and its detail tabs, the filter bar and the element cards, all under the current filter. */
export const SummaryPage = ({
  survey,
  appSetupCompleted,
  locale,
  isReadOnly,
}: {
  survey: TSurvey;
  appSetupCompleted: boolean;
  locale: string;
  isReadOnly: boolean;
}) => {
  const { t } = useTranslation();
  const { filterCriteria } = useResponseFilter();
  const [tab, setTab] = useState<SummaryTab | undefined>(undefined);
  // The previous summary stays on screen while a new filter loads; the metrics show their placeholders meanwhile.
  const summaryQuery = useQuery({
    ...surveySummaryQuery(survey.id, filterCriteria),
    placeholderData: keepPreviousData,
  });
  const displays = useInfiniteQuery({ ...surveyDisplaysQuery(survey.id), enabled: tab === "impressions" });
  const surveySummary = summaryQuery.data ?? EMPTY_SUMMARY;
  const surveyWithRecall = useMemo(() => replaceHeadlineRecall(survey, "default"), [survey]);

  return (
    <>
      <SummaryMetadata
        surveySummary={surveySummary.meta}
        quotasCount={surveySummary.quotas.length}
        isLoading={summaryQuery.isFetching}
        tab={tab}
        setTab={setTab}
      />
      {summaryQuery.isError && (
        <p role="alert" className="text-sm text-red-600">
          {errorMessage(t, summaryQuery.error)}
        </p>
      )}
      {tab === "impressions" && (
        <SummaryImpressions
          displays={displays.data?.pages.flat() ?? []}
          workspaceId={survey.workspaceId}
          isLoading={displays.isPending}
          hasMore={displays.hasNextPage}
          displaysError={displays.isError ? errorMessage(t, displays.error) : null}
          locale={locale}
          onLoadMore={() => void displays.fetchNextPage()}
          onRetry={() => void displays.refetch()}
        />
      )}
      {tab === "dropOffs" && <SummaryDropOffs dropOff={surveySummary.dropOff} survey={surveyWithRecall} />}
      {tab === "quotas" && <QuotasSummary quotas={surveySummary.quotas} />}
      <div className="flex gap-1.5">
        <CustomFilter survey={surveyWithRecall} />
      </div>
      <ScrollToTop />
      <ElementImpressionsProvider dropOff={surveySummary.dropOff}>
        <SummaryList
          summary={surveySummary.summary}
          responseCount={surveySummary.meta.totalResponses}
          survey={surveyWithRecall}
          appSetupCompleted={appSetupCompleted}
          locale={locale}
          isReadOnly={isReadOnly}
        />
      </ElementImpressionsProvider>
    </>
  );
};
