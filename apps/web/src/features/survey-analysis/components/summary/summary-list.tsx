import { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type { TSurvey, TSurveySummary } from "@forma/types/surveys/types";
import { getTextContent } from "@forma/types/surveys/validation";
import { Link } from "@tanstack/react-router";
import { Unplug } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../../components/ui/button";
import { EmptyState } from "../../../../components/ui/empty-state";
import { SkeletonLoader } from "../../../../components/ui/skeleton-loader";
import { getLocalizedValue } from "../../../../lib/survey/i18n";
import { toast } from "../../../../lib/toast";
import { hasActiveFilters, withSummaryFilterRow } from "../../lib/filter-state";
import { OptionsType, type SelectedFilterValue } from "../../lib/filter-types";
import { constructToastMessage } from "../../lib/summary";
import { AnalysisEmptyState } from "../analysis-empty-state";
import { useResponseFilter } from "../filter/response-filter-context";
import {
  MatrixElementSummary,
  MultipleChoiceSummary,
  PictureChoiceSummary,
  RankingSummary,
} from "./choice-summaries";
import {
  ArraySamplesSummary,
  DateElementSummary,
  FileUploadSummary,
  HiddenFieldsSummary,
  OpenTextSummary,
} from "./sample-summaries";
import {
  CESSummary,
  CSATSummary,
  CTASummary,
  CalSummary,
  ConsentSummary,
  NPSSummary,
  RatingSummary,
  SliderSummary,
} from "./score-summaries";
import type { SetSummaryFilter } from "./summary-parts";

/** forma2's EmptyAppSurveys: an app survey of a workspace whose SDK has never connected. */
const EmptyAppSurveys = ({ workspaceId }: { workspaceId: string }) => {
  const { t } = useTranslation();
  return (
    <div className="flex w-full items-center justify-center gap-8 bg-slate-100 py-12">
      <div className="flex size-20 items-center justify-center rounded-full border border-slate-200 bg-white">
        <Unplug size={48} className="text-amber-500" absoluteStrokeWidth />
      </div>
      <div className="flex flex-col">
        <h2 className="text-xl font-semibold text-slate-900">
          {t("workspace.surveys.summary.youre_not_plugged_in_yet")}
        </h2>
        <p className="mt-2 text-sm text-slate-600">
          {t("workspace.surveys.summary.connect_your_website_or_app_with_forma_to_get_started")}
        </p>
        <Button size="sm" className="mt-2 flex w-fit justify-center" asChild>
          <Link to="/workspaces/$workspaceId/integrations/app-connection" params={{ workspaceId }}>
            {t("common.set_up_forma_sdk")}
          </Link>
        </Button>
      </div>
    </div>
  );
};

/** forma2's SummaryList: one card per element summary, in survey order, and the click-to-filter they share. */
export const SummaryList = ({
  summary,
  responseCount,
  survey,
  appSetupCompleted,
  locale,
  isReadOnly,
}: {
  summary: TSurveySummary["summary"];
  responseCount: number;
  survey: TSurvey;
  appSetupCompleted: boolean;
  locale: string;
  isReadOnly: boolean;
}) => {
  const { setSelectedFilter, selectedFilter, dateRange, resetState } = useResponseFilter();
  const { t } = useTranslation();

  const setFilter: SetSummaryFilter = (elementId, label, elementType, filterValue, filterComboBoxValue) => {
    const value = {
      id: elementId,
      label: getTextContent(getLocalizedValue(label, "default")),
      elementType,
      type: OptionsType.ELEMENTS,
    };
    const row = { elementType: value, filterType: { filterComboBoxValue, filterValue } };
    const { rows, replaced } = withSummaryFilterRow(selectedFilter.filter, row);
    const next: SelectedFilterValue = { ...selectedFilter, filter: rows };
    toast.success(
      replaced
        ? t("workspace.surveys.summary.filter_updated_successfully")
        : constructToastMessage(elementType, filterValue, survey, elementId, t, filterComboBoxValue)
    );
    setSelectedFilter(next);
  };

  const renderEntry = (entry: TSurveySummary["summary"][number]) => {
    if (entry.type === "hiddenField") {
      return (
        <HiddenFieldsSummary
          key={entry.id}
          elementSummary={entry}
          workspaceId={survey.workspaceId}
          locale={locale}
        />
      );
    }
    const key = entry.element.id;
    switch (entry.type) {
      case TSurveyElementTypeEnum.OpenText:
        return (
          <OpenTextSummary
            key={key}
            elementSummary={entry}
            survey={survey}
            locale={locale}
            isReadOnly={isReadOnly}
          />
        );
      case TSurveyElementTypeEnum.MultipleChoiceSingle:
      case TSurveyElementTypeEnum.MultipleChoiceMulti:
        return (
          <MultipleChoiceSummary key={key} elementSummary={entry} survey={survey} setFilter={setFilter} />
        );
      case TSurveyElementTypeEnum.NPS:
        return <NPSSummary key={key} elementSummary={entry} survey={survey} setFilter={setFilter} />;
      case TSurveyElementTypeEnum.CTA:
        return <CTASummary key={key} elementSummary={entry} survey={survey} />;
      case TSurveyElementTypeEnum.Rating:
        return <RatingSummary key={key} elementSummary={entry} survey={survey} setFilter={setFilter} />;
      case TSurveyElementTypeEnum.CSAT:
        return <CSATSummary key={key} elementSummary={entry} survey={survey} setFilter={setFilter} />;
      case TSurveyElementTypeEnum.CES:
        return <CESSummary key={key} elementSummary={entry} survey={survey} setFilter={setFilter} />;
      case TSurveyElementTypeEnum.Slider:
        return <SliderSummary key={key} elementSummary={entry} survey={survey} setFilter={setFilter} />;
      case TSurveyElementTypeEnum.Consent:
        return <ConsentSummary key={key} elementSummary={entry} survey={survey} setFilter={setFilter} />;
      case TSurveyElementTypeEnum.PictureSelection:
        return (
          <PictureChoiceSummary key={key} elementSummary={entry} survey={survey} setFilter={setFilter} />
        );
      case TSurveyElementTypeEnum.Date:
        return <DateElementSummary key={key} elementSummary={entry} survey={survey} locale={locale} />;
      case TSurveyElementTypeEnum.FileUpload:
        return <FileUploadSummary key={key} elementSummary={entry} survey={survey} locale={locale} />;
      case TSurveyElementTypeEnum.Cal:
        return <CalSummary key={key} elementSummary={entry} survey={survey} />;
      case TSurveyElementTypeEnum.Matrix:
        return (
          <MatrixElementSummary key={key} elementSummary={entry} survey={survey} setFilter={setFilter} />
        );
      case TSurveyElementTypeEnum.Address:
      case TSurveyElementTypeEnum.ContactInfo:
        return <ArraySamplesSummary key={key} elementSummary={entry} survey={survey} locale={locale} />;
      case TSurveyElementTypeEnum.Ranking:
        return <RankingSummary key={key} elementSummary={entry} survey={survey} />;
      default:
        return null;
    }
  };

  const renderContent = () => {
    if (survey.type === "app" && responseCount === 0 && !appSetupCompleted) {
      return <EmptyAppSurveys workspaceId={survey.workspaceId} />;
    }
    if (summary.length === 0) return <SkeletonLoader type="summary" />;
    if (responseCount === 0) {
      // A filter that matches nothing is not an empty survey: say so, and offer the way back.
      if (hasActiveFilters(selectedFilter, dateRange)) {
        return (
          <div className="rounded-xl border border-slate-200 shadow-xs">
            <AnalysisEmptyState
              title={t("workspace.surveys.responses.no_responses_match_filters")}
              action={
                <Button variant="secondary" size="sm" onClick={resetState}>
                  {t("common.clear_filters")}
                </Button>
              }
            />
          </div>
        );
      }
      return <EmptyState text={t("workspace.surveys.summary.no_responses_found")} />;
    }
    return summary.map(renderEntry);
  };

  return <div className="mt-10 space-y-8">{renderContent()}</div>;
};
