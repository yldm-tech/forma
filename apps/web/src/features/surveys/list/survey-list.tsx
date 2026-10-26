import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import {
  ChevronDownIcon,
  InfoIcon,
  LayoutTemplateIcon,
  PlusCircleIcon,
  SparklesIcon,
  XIcon,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Alert, AlertDescription, AlertTitle } from "../../../components/ui/alert";
import { Button } from "../../../components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "../../../components/ui/dropdown-menu";
import { EmptyState } from "../../../components/ui/empty-state";
import { PageContentWrapper, PageHeader } from "../../../components/ui/page";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "../../../components/ui/tooltip";
import { cn } from "../../../lib/cn";
import { readStorage, writeStorage } from "../../../lib/storage";
import { workspaceAIQuery } from "../../ai/availability";
import { CreateWithAIDialog } from "../ai-create/create-with-ai-dialog";
import { CUSTOM_TEMPLATE_ID, publicDomainQuery, surveySdkStateQuery, v3ErrorMessage } from "../api";
import { TemplateGallery } from "../templates/template-gallery";
import { useCreateSurveyFromTemplate } from "../templates/use-create-survey";
import {
  SURVEYS_PER_PAGE,
  type SurveyFilters as Filters,
  type WorkspaceChannel,
  hasActiveFilters,
  initialFilters,
  normalizeSurveyFilters,
  restoredSurveyFilters,
  storableSurveyFilters,
  surveyFiltersKey,
} from "./filters";
import {
  useArchiveSurvey,
  useCopySurvey,
  useDeleteSurvey,
  useRenameSurvey,
  useRestoreSurvey,
  useSurveys,
  useUpdateSurveyStatus,
} from "./hooks";
import { SURVEY_TABLE_COLUMN, SURVEY_TABLE_GRID, SURVEY_TABLE_ROW_PADDING, SurveyCard } from "./survey-card";
import type { SurveyActions } from "./survey-dropdown-menu";
import { SurveyFilters } from "./survey-filters";

/** forma2's NewSurveyMenu: a draft written by AI, the template gallery, or a blank survey opened in the editor. */
const NewSurveyMenu = ({
  workspaceId,
  organizationId,
  surveyType,
  defaultLanguage,
}: {
  workspaceId: string;
  organizationId: string;
  surveyType: "link" | "app";
  defaultLanguage: string;
}) => {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const { create, isPending } = useCreateSurveyFromTemplate();
  const [isAIDialogOpen, setIsAIDialogOpen] = useState(false);
  const ai = useQuery(workspaceAIQuery(workspaceId)).data;
  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button size="sm">
            {t("workspace.surveys.new_survey")}
            <ChevronDownIcon />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-52">
          <DropdownMenuItem
            icon={<SparklesIcon className="size-4" />}
            onSelect={() => setIsAIDialogOpen(true)}>
            {t("workspace.surveys.ai_create.create_with_ai")}
          </DropdownMenuItem>
          <DropdownMenuItem
            icon={<LayoutTemplateIcon className="size-4" />}
            onSelect={() => void navigate({ href: `/workspaces/${workspaceId}/surveys/templates` })}>
            {t("workspace.surveys.ai_create.choose_template")}
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={isPending}
            icon={<PlusCircleIcon className="size-4" />}
            onSelect={(event) => {
              event.preventDefault();
              void create({
                workspaceId,
                templateId: CUSTOM_TEMPLATE_ID,
                source: "custom",
                surveyType,
                defaultLanguage,
              });
            }}>
            {t("workspace.surveys.ai_create.start_from_scratch")}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      {isAIDialogOpen && (
        <CreateWithAIDialog
          open
          onOpenChange={setIsAIDialogOpen}
          workspaceId={workspaceId}
          organizationId={organizationId}
          locale={i18n.language}
          surveyType={surveyType}
          isAIAvailable={ai?.available ?? false}
          aiUnavailableReason={ai?.reason}
        />
      )}
    </>
  );
};

const SurveyLoading = () => (
  <div className="grid h-full w-full animate-pulse place-content-stretch gap-4" aria-hidden="true">
    {[1, 2, 3, 4, 5].map((i) => (
      <div
        key={i}
        className="flex h-16 items-center justify-between rounded-xl border border-slate-200 bg-white p-4 shadow-xs">
        <div className="h-4 w-32 rounded-xl bg-slate-400" />
        {[1, 2, 3, 4, 5].map((j) => (
          <div key={j} className="h-4 w-20 rounded-xl bg-slate-200" />
        ))}
        <div className="size-8 rounded-md bg-slate-300" />
      </div>
    ))}
  </div>
);

/** The survey table's header row; the completed column explains what it counts, since its label is also a status. */
const SurveyTableHeader = () => {
  const { t } = useTranslation();
  const cell = "flex min-w-0 justify-self-center text-center";
  return (
    <div role="rowgroup">
      <div
        role="row"
        className={cn(
          SURVEY_TABLE_GRID,
          SURVEY_TABLE_ROW_PADDING,
          // On a phone the rows are two-line cards whose cells say what they show, so there is no header row.
          "border-transparent text-sm text-slate-800 max-sm:hidden"
        )}>
        <div role="columnheader" className="min-w-0">
          {t("common.name")}
        </div>
        <div role="columnheader" className={cell}>
          {t("common.status")}
        </div>
        <div role="columnheader" className={cell}>
          {/* The label is the tooltip's trigger, so the column is named by the label alone (a screen reader repeats it with every cell) and the hint is only the button's description. */}
          <TooltipProvider delayDuration={200}>
            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  type="button"
                  className="focus-visible:ring-ring inline-flex min-w-0 items-center gap-1 rounded-sm text-center break-words focus-visible:ring-2 focus-visible:outline-hidden">
                  {t("workspace.surveys.completed_responses")}
                  <InfoIcon className="size-3.5 shrink-0 text-slate-500" aria-hidden="true" />
                </button>
              </TooltipTrigger>
              <TooltipContent className="max-w-64">
                {t("workspace.surveys.completed_responses_hint")}
              </TooltipContent>
            </Tooltip>
          </TooltipProvider>
        </div>
        <div role="columnheader" className={cn(cell, SURVEY_TABLE_COLUMN.lgOnly)}>
          {t("common.type")}
        </div>
        <div role="columnheader" className={cn(cell, SURVEY_TABLE_COLUMN.wideOnly)}>
          {t("common.created_at")}
        </div>
        <div role="columnheader" className={cn(cell, SURVEY_TABLE_COLUMN.lgOnly)}>
          {t("common.updated_at")}
        </div>
        <div role="columnheader" className={cn(cell, SURVEY_TABLE_COLUMN.wideOnly)}>
          {t("common.created_by")}
        </div>
        <div role="columnheader">
          <span className="sr-only">{t("common.actions")}</span>
        </div>
      </div>
    </div>
  );
};

/**
 * The live app surveys the SDK never receives: its workspace state lists only the newest few, and an older survey past that cap is silently never shown. Names the newest of them, each linked to its summary, so the creator can complete or pause the ones they no longer need.
 */
const SdkStateWarning = ({ workspaceId }: { workspaceId: string }) => {
  const { t } = useTranslation();
  const state = useQuery(surveySdkStateQuery(workspaceId)).data;
  if (!state || state.excludedCount === 0) return null;
  const unnamed = state.excludedCount - state.excluded.length;
  return (
    <Alert variant="warning" role="status" data-testid="sdk-state-truncated">
      <AlertTitle>
        {t("workspace.surveys.list.state_truncated_title", { count: state.excludedCount })}
      </AlertTitle>
      <AlertDescription>
        <p>{t("workspace.surveys.list.state_truncated", { limit: state.limit })}</p>
        <ul className="mt-1 list-disc pl-5">
          {state.excluded.map((survey) => (
            <li key={survey.id}>
              <Link
                to="/workspaces/$workspaceId/surveys/$surveyId/summary"
                params={{ workspaceId, surveyId: survey.id }}
                className="bg-transparent! underline">
                {survey.name}
              </Link>
            </li>
          ))}
        </ul>
        {unnamed > 0 && <p>{t("workspace.surveys.list.state_truncated_more", { count: unnamed })}</p>}
      </AlertDescription>
    </Alert>
  );
};

/**
 * forma2's SurveysList: the workspace's surveys under the filters this browser last used in this workspace, a page at a time, with their actions. A workspace without any survey shows the template gallery instead (or, to a read-only member, that they cannot create one).
 */
export const SurveysList = ({
  workspaceId,
  organizationId,
  isReadOnly,
  canSaveAsTemplate,
  channel,
  surveyType,
  defaultLanguage,
  readOnlyEmptyText,
}: {
  workspaceId: string;
  organizationId: string;
  isReadOnly: boolean;
  /** What a read-only list says in a workspace without any survey; by default that a read-only member cannot create one. */
  readOnlyEmptyText?: string;
  /** The caller is an owner or a manager of the organization, whose rows offer "Save as template". */
  canSaveAsTemplate: boolean;
  channel: WorkspaceChannel;
  surveyType: "link" | "app";
  defaultLanguage: string;
}) => {
  const { t } = useTranslation();
  // The filters this browser used last in this workspace, without a name search; a stored value that cannot be read gives way to the defaults, which the write below then stores over it.
  const [filters, setFilters] = useState<Filters>(() =>
    restoredSurveyFilters(readStorage(surveyFiltersKey(workspaceId)), channel)
  );
  // Bumped by "Clear filters" to remount the toolbar, which holds the search text still being typed.
  const [filtersResetCount, setFiltersResetCount] = useState(0);
  const publicDomain = useQuery(publicDomainQuery).data;
  const normalized = useMemo(() => normalizeSurveyFilters(filters, channel), [filters, channel]);

  useEffect(
    () => writeStorage(surveyFiltersKey(workspaceId), JSON.stringify(storableSurveyFilters(normalized))),
    [workspaceId, normalized]
  );

  const clearFilters = () => {
    setFilters(initialFilters);
    setFiltersResetCount((count) => count + 1);
  };

  // Clear filters takes away the button that was pressed (the toolbar's and the empty state's both go once nothing is filtered), so the focus moves to the search box of the remounted toolbar rather than falling to the page.
  const searchInputRef = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (filtersResetCount > 0) searchInputRef.current?.focus();
  }, [filtersResetCount]);

  const list = useSurveys({
    workspaceId,
    limit: SURVEYS_PER_PAGE,
    filters: normalized,
  });
  const remove = useDeleteSurvey(list.queryKey);
  const archive = useArchiveSurvey(list.queryKey);
  const restore = useRestoreSurvey(list.queryKey);
  const rename = useRenameSurvey(list.queryKey);
  const updateStatus = useUpdateSurveyStatus(list.queryKey);
  const copy = useCopySurvey();
  const actions: SurveyActions = {
    deleteSurvey: (surveyId) => remove.mutateAsync({ surveyId }),
    archiveSurvey: (surveyId) => archive.mutateAsync({ surveyId }),
    restoreSurvey: (surveyId) => restore.mutateAsync({ surveyId }),
    renameSurvey: (surveyId, name) => rename.mutateAsync({ surveyId, name }),
    updateSurveyStatus: (surveyId, status) => updateStatus.mutateAsync({ surveyId, status }),
    copySurvey: (input) => copy.mutateAsync(input),
  };

  // Only a workspace without a single survey gets the empty states; any other empty list keeps the toolbar, so the filter that emptied it stays reachable.
  const isWorkspaceEmpty = !list.isError && list.workspaceSurveyCount === 0;

  // "Load more" hands the focus to the first survey it added; without one (the page held only surveys being removed), to the count above the table. The button itself is gone once the last page is in.
  const tableRef = useRef<HTMLDivElement>(null);
  const countRef = useRef<HTMLParagraphElement>(null);
  const focusRowAfterLoad = useRef<number | null>(null);
  const loadMore = async () => {
    focusRowAfterLoad.current = list.surveys.length;
    await list.fetchNextPage();
  };
  const loadedCount = list.surveys.length;
  useEffect(() => {
    const index = focusRowAfterLoad.current;
    if (index === null || list.isFetchingNextPage) return;
    focusRowAfterLoad.current = null;
    const names = tableRef.current?.querySelectorAll<HTMLElement>("[data-survey-name]");
    (names?.[index] ?? countRef.current)?.focus();
  }, [loadedCount, list.isFetchingNextPage]);

  if (list.isLoading && list.surveys.length === 0) {
    return (
      <PageContentWrapper>
        <PageHeader pageTitle={t("common.surveys")} />
        <div className="flex h-9 animate-pulse gap-2" aria-hidden="true">
          <div className="w-48 rounded-md bg-slate-300" />
          {[1, 2, 3].map((i) => (
            <div key={i} className="w-24 rounded-md bg-slate-300" />
          ))}
        </div>
        <SurveyLoading />
      </PageContentWrapper>
    );
  }

  if (isWorkspaceEmpty && !isReadOnly) {
    return (
      <TemplateGallery
        workspaceId={workspaceId}
        organizationId={organizationId}
        surveyType={surveyType}
        defaultLanguage={defaultLanguage}
        isTemplatePage={false}
      />
    );
  }

  if (isWorkspaceEmpty) {
    return (
      <PageContentWrapper>
        <PageHeader pageTitle={t("common.surveys")} />
        <div className="rounded-xl border border-slate-100 bg-white p-4 shadow-2xs">
          <div className="w-full space-y-3">
            <div className="h-16 w-full rounded-lg bg-slate-50" />
            <div className="flex h-16 w-full flex-col items-center justify-center rounded-lg bg-slate-50 text-sm text-slate-500">
              {readOnlyEmptyText ??
                t("workspace.surveys.read_only_user_not_allowed_to_create_survey_warning")}
            </div>
            <div className="h-16 w-full rounded-lg bg-slate-50" />
          </div>
        </div>
      </PageContentWrapper>
    );
  }

  // An empty list under filters says so and offers to clear them; one without (a workspace whose surveys are all archived) only that there is nothing to show.
  let content = hasActiveFilters(normalized) ? (
    <div className="flex flex-col items-center gap-4">
      <div className="w-full">
        <EmptyState variant="simple" text={t("workspace.surveys.no_surveys_match_filters")} />
      </div>
      <Button variant="secondary" size="sm" onClick={clearFilters}>
        {t("common.clear_filters")}
        <XIcon />
      </Button>
    </div>
  ) : (
    <EmptyState variant="simple" text={t("common.no_surveys_found")} />
  );
  if (list.isError && list.surveys.length === 0) {
    content = (
      <div className="flex w-full flex-col items-center justify-center gap-4 py-16 text-slate-600">
        <p role="alert">{v3ErrorMessage(list.error, t("common.something_went_wrong_please_try_again"))}</p>
        <Button variant="secondary" size="sm" onClick={() => void list.refetch()}>
          {t("common.try_again")}
        </Button>
      </div>
    );
  } else if (list.surveys.length > 0) {
    content = (
      <div>
        {/* Positioned, so the visually hidden labels inside it stay within the scrolling page instead of widening the document. */}
        <div ref={tableRef} role="table" aria-label={t("common.surveys")} className="relative space-y-3">
          <SurveyTableHeader />
          <div role="rowgroup" className="space-y-3">
            {list.surveys.map((survey) => (
              <SurveyCard
                key={survey.id}
                survey={survey}
                workspaceId={workspaceId}
                organizationId={organizationId}
                isReadOnly={isReadOnly}
                publicDomain={publicDomain}
                actions={actions}
                canSaveAsTemplate={canSaveAsTemplate}
              />
            ))}
          </div>
        </div>
        {list.hasNextPage && (
          <div className="flex justify-center py-5">
            <Button
              variant="secondary"
              size="sm"
              loading={list.isFetchingNextPage}
              onClick={() => void loadMore()}>
              {t("common.load_more")}
            </Button>
          </div>
        )}
      </div>
    );
  }

  return (
    <PageContentWrapper>
      <PageHeader
        pageTitle={t("common.surveys")}
        cta={
          isReadOnly ? undefined : (
            <NewSurveyMenu
              workspaceId={workspaceId}
              organizationId={organizationId}
              surveyType={surveyType}
              defaultLanguage={defaultLanguage}
            />
          )
        }
      />
      <div className="space-y-6">
        {channel !== "link" && <SdkStateWarning workspaceId={workspaceId} />}
        <SurveyFilters
          key={filtersResetCount}
          filters={normalized}
          setFilters={setFilters}
          channel={channel}
          onClear={clearFilters}
          searchInputRef={searchInputRef}
        />
        <div>
          {/* Announced as it changes: how much of the filtered list is on the page. */}
          <p
            ref={countRef}
            tabIndex={-1}
            aria-live="polite"
            className="min-h-5 text-sm text-slate-500 outline-hidden">
            {list.totalCount !== null &&
              list.totalCount > 0 &&
              t("workspace.surveys.showing_count", {
                shown: Math.min(loadedCount, list.totalCount),
                total: list.totalCount,
              })}
          </p>
          <div className="mt-3">{content}</div>
        </div>
      </div>
    </PageContentWrapper>
  );
};
