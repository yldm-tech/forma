import type { TSurvey, TSurveySummary } from "@forma/types/surveys/types";
import { EyeOffIcon, InboxIcon, MessageSquareTextIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { ElementSummaryHeader } from "./summary-parts";

/** A card of a read-only results link that lists individual answers the link does not show: its header and counts, and a note in place of the answers. */
export const WithheldSummary = ({
  entry,
  survey,
}: {
  entry: TSurveySummary["summary"][number];
  survey: TSurvey;
}) => {
  const { t } = useTranslation();
  return (
    <div className="rounded-xl border border-slate-200 bg-white shadow-xs" data-testid="withheld-summary">
      {entry.type === "hiddenField" ? (
        <div className="space-y-2 px-4 pt-6 pb-5 md:px-6">
          <h3 className="pb-1 text-lg font-semibold text-slate-900 md:text-xl">{entry.id}</h3>
          <div className="flex gap-x-2 text-xs font-semibold text-slate-600 md:text-sm">
            <div className="flex items-center rounded-lg bg-slate-100 p-2">
              <MessageSquareTextIcon className="mr-2 size-4" />
              {t("common.hidden_field")}
            </div>
            <div className="flex items-center rounded-lg bg-slate-100 p-2">
              <InboxIcon className="mr-2 size-4" />
              {t("common.count_responses", { count: entry.responseCount })}
            </div>
          </div>
        </div>
      ) : (
        <ElementSummaryHeader elementSummary={entry} survey={survey} />
      )}
      <div className="flex items-center gap-3 border-t border-slate-200 bg-slate-50 px-4 py-4 text-sm text-slate-600 md:px-6">
        <EyeOffIcon className="size-4 shrink-0 text-slate-500" aria-hidden="true" />
        <p>{t("workspace.analysis.result_shares.withheld_card")}</p>
      </div>
    </div>
  );
};
