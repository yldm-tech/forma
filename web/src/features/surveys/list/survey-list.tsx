import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { ChevronDownIcon, LayoutTemplateIcon, PlusCircleIcon } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "../../../components/ui/dropdown-menu";
import { PageContentWrapper, PageHeader } from "../../../components/ui/page";
import { readStorage, writeStorage } from "../../../lib/storage";
import { CUSTOM_TEMPLATE_ID, publicDomainQuery, v3ErrorMessage } from "../api";
import { TemplateGallery } from "../templates/template-gallery";
import { useCreateSurveyFromTemplate } from "../templates/use-create-survey";
import {
  SURVEYS_PER_PAGE,
  SURVEY_FILTERS_KEY,
  type SurveyFilters as Filters,
  type WorkspaceChannel,
  initialFilters,
  normalizeSurveyFilters,
  parseStoredSurveyFilters,
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
import { SurveyCard } from "./survey-card";
import type { SurveyActions } from "./survey-dropdown-menu";
import { SurveyFilters } from "./survey-filters";

/** forma2's NewSurveyMenu, without "Create with AI", which this server does not offer: the template gallery, or a blank survey opened in the editor. */
const NewSurveyMenu = ({
  workspaceId,
  surveyType,
  defaultLanguage,
}: {
  workspaceId: string;
  surveyType: "link" | "app";
  defaultLanguage: string;
}) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { create, isPending } = useCreateSurveyFromTemplate();
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button size="sm">
          {t("workspace.surveys.new_survey")}
          <ChevronDownIcon />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-52">
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

/**
 * forma2's SurveysList: the workspace's surveys under the filters this browser last used, a page at a time, with their actions. A workspace without any survey shows the template gallery instead (or, to a read-only member, that they cannot create one).
 */
export const SurveysList = ({
  workspaceId,
  organizationId,
  isReadOnly,
  channel,
  surveyType,
  defaultLanguage,
}: {
  workspaceId: string;
  organizationId: string;
  isReadOnly: boolean;
  channel: WorkspaceChannel;
  surveyType: "link" | "app";
  defaultLanguage: string;
}) => {
  const { t } = useTranslation();
  // The filters this browser used last; a stored value that cannot be read gives way to the defaults, which the write below then stores over it.
  const [filters, setFilters] = useState<Filters>(
    () => parseStoredSurveyFilters(readStorage(SURVEY_FILTERS_KEY), channel) ?? initialFilters
  );
  const publicDomain = useQuery(publicDomainQuery).data;
  const normalized = useMemo(() => normalizeSurveyFilters(filters, channel), [filters, channel]);

  useEffect(() => writeStorage(SURVEY_FILTERS_KEY, JSON.stringify(normalized)), [normalized]);

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
              {t("workspace.surveys.read_only_user_not_allowed_to_create_survey_warning")}
            </div>
            <div className="h-16 w-full rounded-lg bg-slate-50" />
          </div>
        </div>
      </PageContentWrapper>
    );
  }

  let content = (
    <div className="flex w-full flex-col items-center justify-center text-slate-600">
      <span className="size-24 p-4 text-center text-5xl" aria-hidden="true">
        🕵️
      </span>
      {t("common.no_surveys_found")}
    </div>
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
        <div className="flex-col space-y-3">
          <div className="mt-6 grid w-full grid-cols-8 place-items-center gap-3 px-6 pr-8 text-sm text-slate-800">
            <div className="col-span-2 place-self-start">{t("common.name")}</div>
            <div className="col-span-1">{t("common.status")}</div>
            <div className="col-span-1">{t("workspace.surveys.completed_responses")}</div>
            <div className="col-span-1">{t("common.type")}</div>
            <div className="col-span-1">{t("common.created_at")}</div>
            <div className="col-span-1">{t("common.updated_at")}</div>
            <div className="col-span-1">{t("common.created_by")}</div>
          </div>
          {list.surveys.map((survey) => (
            <SurveyCard
              key={survey.id}
              survey={survey}
              workspaceId={workspaceId}
              organizationId={organizationId}
              isReadOnly={isReadOnly}
              publicDomain={publicDomain}
              actions={actions}
            />
          ))}
        </div>
        {list.hasNextPage && (
          <div className="flex justify-center py-5">
            <Button
              variant="secondary"
              size="sm"
              loading={list.isFetchingNextPage}
              onClick={() => void list.fetchNextPage()}>
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
              surveyType={surveyType}
              defaultLanguage={defaultLanguage}
            />
          )
        }
      />
      <div className="space-y-6">
        <SurveyFilters filters={normalized} setFilters={setFilters} channel={channel} />
        {content}
      </div>
    </PageContentWrapper>
  );
};
