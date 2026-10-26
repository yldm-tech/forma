import type { TResponseWithQuotas } from "@forma/types/responses";
import type { TSurvey } from "@forma/types/surveys/types";
import type { TTag } from "@forma/types/tags";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../../components/ui/button";
import { SingleResponseCard } from "../response-card/single-response-card";

/**
 * The responses on a phone-sized screen, where the table's columns do not fit: the card the table opens for a response, one under another, newest first, under how many of the matching responses are loaded, with Load more for the next page. While the first page loads, placeholder cards stand in; with nothing to show, the page's empty state.
 */
export const ResponseCardList = ({
  survey,
  responses,
  environmentTags,
  isReadOnly,
  locale,
  isFetchingFirstPage,
  hasMore,
  isFetchingMore,
  fetchNextPage,
  countSummary,
  emptyState,
  onResponseChanged,
  onResponsesDeleted,
}: {
  survey: TSurvey;
  responses: TResponseWithQuotas[];
  environmentTags: TTag[];
  isReadOnly: boolean;
  locale: string;
  isFetchingFirstPage: boolean;
  hasMore: boolean;
  isFetchingMore: boolean;
  fetchNextPage: () => void;
  /** How many responses are loaded of those matching. */
  countSummary: ReactNode;
  emptyState: ReactNode;
  onResponseChanged: (responseId: string) => void;
  onResponsesDeleted: (responseIds: string[]) => void;
}) => {
  const { t } = useTranslation();

  if (isFetchingFirstPage) {
    return (
      <div role="status" aria-busy="true" aria-label={t("common.loading")} className="space-y-4">
        {[0, 1, 2].map((index) => (
          <div key={index} className="h-40 animate-pulse rounded-xl bg-slate-200" />
        ))}
      </div>
    );
  }
  if (responses.length === 0) return <>{emptyState}</>;

  return (
    <div className="space-y-4">
      {countSummary}
      <ul className="space-y-4" aria-label={t("common.responses")}>
        {responses.map((response) => (
          <li key={response.id}>
            <SingleResponseCard
              survey={survey}
              response={response}
              environmentTags={environmentTags}
              isReadOnly={isReadOnly}
              locale={locale}
              onResponseChanged={onResponseChanged}
              onResponsesDeleted={onResponsesDeleted}
            />
          </li>
        ))}
      </ul>
      {hasMore && (
        <div className="flex justify-center">
          <Button loading={isFetchingMore} onClick={fetchNextPage}>
            {t("common.load_more")}
          </Button>
        </div>
      )}
    </div>
  );
};
