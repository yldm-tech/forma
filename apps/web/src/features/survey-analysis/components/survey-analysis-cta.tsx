import type { TSurvey } from "@forma/types/surveys/types";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useLocation, useNavigate, useRouter } from "@tanstack/react-router";
import {
  BellRing,
  Eye,
  ListRestart,
  MoreHorizontalIcon,
  RefreshCcwIcon,
  SparklesIcon,
  SquarePenIcon,
} from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../components/ui/button";
import { ConfirmationModal } from "../../../components/ui/confirmation-modal";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "../../../components/ui/dropdown-menu";
import { IconBar, type IconAction } from "../../../components/ui/icon-bar";
import { ApiError } from "../../../lib/api";
import { errorMessage } from "../../../lib/errors";
import type { Me } from "../../../lib/queries/auth";
import type { WorkspaceDetail } from "../../../lib/queries/workspaces";
import { toast } from "../../../lib/toast";
import { getAIUnavailableMessage, workspaceAIQuery } from "../../ai/availability";
import { EditPublicSurveyAlertDialog } from "../../survey-editor/components/edit-public-survey-alert-dialog";
import { surveysApi } from "../../surveys/api";
import { surveyKeys } from "../../surveys/list/query";
import { analysisApi, invalidateSurveyAnalysis } from "../api";
import { ShareSurveyModal } from "./share/share-survey-modal";
import { useSingleUseId } from "./share/use-single-use-id";
import { SuccessMessage } from "./success-message";
import { SurveyStatusDropdown } from "./survey-status-dropdown";

/** The search params the analysis views read: the filter, the share modal, and the publish result the editor hands over. */
export interface AnalysisSearch {
  filter?: unknown;
  share?: boolean;
  success?: boolean;
  scheduled?: boolean;
}

/** How long each status line holds before the next, forma2's AI_STATUS_DEFAULT_CADENCE_MS; the last one holds until the answer, since cycling back would read as the work restarting. */
export const EXAMPLE_STATUS_CADENCE_MS = 2500;

/**
 * forma2's example-responses action of the analysis toolbar: AI-written responses for a survey that has none, so its summary can be explored before it is shared. It is offered to writers of a survey that is not archived, and disabled while AI is unavailable to the workspace, once the survey has responses, and while a generation runs. The wait is shown as status lines in the loading toast, which turns into the result in place.
 */
export const useExampleResponsesAction = ({
  survey,
  workspaceId,
  responseCount,
  isReadOnly,
  onGenerated,
}: {
  survey: TSurvey;
  workspaceId: string;
  responseCount: number;
  isReadOnly: boolean;
  onGenerated: () => Promise<unknown>;
}): IconAction => {
  const { t } = useTranslation();
  const ai = useQuery({ ...workspaceAIQuery(workspaceId), enabled: !isReadOnly && !survey.archivedAt }).data;
  const [isGenerating, setIsGenerating] = useState(false);
  const isAIUnavailable = ai?.available !== true;

  // The generation is one request the server cancels when the tab goes away, so a reload mid-run throws away answers the user is waiting on and cannot cheaply ask for again.
  useEffect(() => {
    if (!isGenerating) return;
    const warn = (event: BeforeUnloadEvent) => event.preventDefault();
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [isGenerating]);

  const generate = async () => {
    if (isGenerating) return;
    setIsGenerating(true);
    const id = toast.loading(t("workspace.surveys.summary.ai_status_reading_survey"));
    const nextStatuses = [
      t("workspace.surveys.summary.ai_status_drafting_answers"),
      t("workspace.surveys.summary.ai_status_saving_responses"),
    ];
    const cadence = setInterval(() => {
      const next = nextStatuses.shift();
      if (next !== undefined) toast.loading(next, { id });
    }, EXAMPLE_STATUS_CADENCE_MS);
    try {
      const result = await analysisApi.generateExampleResponses(survey.id);
      clearInterval(cadence);
      toast.success(
        t("workspace.surveys.summary.example_responses_generated_successfully", {
          count: result.createdCount,
        }),
        { id }
      );
      await onGenerated();
    } catch (error) {
      clearInterval(cadence);
      // The server's reason when it refused (AI switched off, the survey answered meanwhile, the rate limit), forma2's wording for anything else.
      const refused = error instanceof ApiError && error.status < 500;
      toast.error(
        refused ? errorMessage(t, error) : t("workspace.surveys.summary.example_responses_generation_failed"),
        { id }
      );
    } finally {
      setIsGenerating(false);
    }
  };

  const tooltip = (() => {
    if (isGenerating) return t("workspace.surveys.summary.generating_example_responses");
    if (ai && !ai.available) return getAIUnavailableMessage(ai.reason, t);
    if (responseCount > 0)
      return t("workspace.surveys.summary.generate_example_responses_disabled_has_responses");
    return t("workspace.surveys.summary.generate_example_responses");
  })();

  return {
    icon: SparklesIcon,
    tooltip,
    onClick: () => void generate(),
    disabled: isGenerating || isAIUnavailable || responseCount > 0,
    isVisible: !isReadOnly && !survey.archivedAt,
  };
};

/** forma2's SurveyAnalysisCTA: the status, the tool icons, and the share modal of a survey's analysis header. */
export const SurveyAnalysisCTA = ({
  survey,
  workspace,
  user,
  publicDomain,
  responseCount,
}: {
  survey: TSurvey;
  workspace: WorkspaceDetail;
  user: Me;
  publicDomain: string;
  responseCount: number;
}) => {
  const { t } = useTranslation();
  const router = useRouter();
  const navigate = useNavigate();
  const location = useLocation();
  const queryClient = useQueryClient();
  const search = location.search as AnalysisSearch & Record<string, unknown>;
  const isReadOnly = workspace.access.isReadOnly;
  const [isShareOpen, setIsShareOpen] = useState(false);
  const [isResetModalOpen, setIsResetModalOpen] = useState(false);
  const [isResetting, setIsResetting] = useState(false);
  const [isRefreshing, setIsRefreshing] = useState(false);
  const [isCautionDialogOpen, setIsCautionDialogOpen] = useState(false);
  const [isDuplicating, setIsDuplicating] = useState(false);
  const { refreshSingleUseId } = useSingleUseId(survey, isReadOnly);

  const appSetupCompleted = survey.type === "app" && workspace.appSetupCompleted;
  const isStartView = search.share === true;
  const editHref = `/workspaces/${workspace.id}/surveys/${survey.id}/edit`;

  /** Rewrites the analysis URL's search params in place, keeping the filter. */
  const setSearch = (patch: Partial<AnalysisSearch>, replace: boolean) => {
    const nextSearch = { ...search, ...patch };
    void router.navigate({
      href: `${location.pathname}${router.options.stringifySearch(nextSearch)}`,
      replace,
    });
  };

  const refreshAnalysis = () => invalidateSurveyAnalysis(queryClient, survey.id);

  const exampleResponsesAction = useExampleResponsesAction({
    survey,
    workspaceId: workspace.id,
    responseCount,
    isReadOnly,
    onGenerated: refreshAnalysis,
  });

  const duplicateSurveyAndRoute = async () => {
    setIsDuplicating(true);
    try {
      const duplicated = await surveysApi.copy(survey.id, workspace.id);
      toast.success(t("workspace.surveys.survey_duplicated_successfully"));
      void queryClient.invalidateQueries({ queryKey: surveyKeys.lists() });
      await navigate({ href: `/workspaces/${workspace.id}/surveys/${duplicated.id}/edit` });
    } catch (error) {
      toast.error(errorMessage(t, error));
    } finally {
      setIsCautionDialogOpen(false);
      setIsDuplicating(false);
    }
  };

  const getPreviewUrl = async () => {
    const surveyUrl = new URL(`${publicDomain}/s/${survey.id}`);
    if (survey.singleUse?.enabled) {
      const singleUseLinkParams = await refreshSingleUseId();
      if (singleUseLinkParams) {
        surveyUrl.searchParams.set("suId", singleUseLinkParams.suId);
        if (singleUseLinkParams.suToken) surveyUrl.searchParams.set("suToken", singleUseLinkParams.suToken);
      }
    }
    surveyUrl.searchParams.set("preview", "true");
    return surveyUrl.toString();
  };

  const handleResetSurvey = async () => {
    setIsResetting(true);
    try {
      const result = await analysisApi.resetSurvey(survey.id);
      toast.success(
        t("workspace.surveys.summary.survey_reset_successfully", {
          responseCount: result.deletedResponsesCount,
          displayCount: result.deletedDisplaysCount,
        })
      );
      await refreshAnalysis();
    } catch (error) {
      toast.error(errorMessage(t, error));
    } finally {
      setIsResetting(false);
      setIsResetModalOpen(false);
    }
  };

  const iconActions: IconAction[] = [
    {
      icon: RefreshCcwIcon,
      tooltip: t("common.refresh"),
      onClick: async () => {
        if (isRefreshing) return;
        setIsRefreshing(true);
        try {
          await refreshAnalysis();
          toast.success(t("common.data_refreshed_successfully"));
        } catch (error) {
          toast.error(errorMessage(t, error));
        } finally {
          setIsRefreshing(false);
        }
      },
      disabled: isRefreshing,
      isVisible: true,
    },
    {
      icon: BellRing,
      tooltip: t("workspace.surveys.summary.configure_alerts"),
      onClick: () => void navigate({ to: "/account/settings/notifications" }),
      isVisible: !isReadOnly && !survey.archivedAt,
    },
    {
      icon: Eye,
      tooltip: t("common.preview"),
      onClick: async () => {
        const previewUrl = await getPreviewUrl();
        window.open(previewUrl, "_blank", "noopener");
      },
      isVisible: survey.type === "link" && !survey.archivedAt,
    },
    exampleResponsesAction,
    {
      icon: SquarePenIcon,
      tooltip: t("common.edit"),
      onClick: () => {
        if (responseCount > 0) setIsCautionDialogOpen(true);
        else void navigate({ href: editHref });
      },
      // Archived surveys are read-only; editing is refused by the server, so the entry point is hidden too.
      isVisible: !isReadOnly && !survey.archivedAt,
    },
  ];

  // Reset deletes every response, so it is a labelled, destructive item of the More menu rather than an icon beside Edit. Below lg the menu also holds the icon actions, so the status, the menu and Share fit a tablet's header.
  const canReset = !isReadOnly && !survey.archivedAt;
  const visibleIconActions = iconActions.filter((action) => action.isVisible);

  return (
    <div className="hidden items-center justify-end gap-x-1.5 sm:flex">
      {!isReadOnly && (appSetupCompleted || survey.type === "link") && survey.status !== "draft" && (
        <SurveyStatusDropdown survey={survey} />
      )}

      <div className="hidden lg:flex">
        <IconBar actions={iconActions} ariaLabel={t("common.actions")} />
      </div>
      {(canReset || visibleIconActions.length > 0) && (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              variant="outline"
              size="icon"
              className={canReset ? undefined : "lg:hidden"}
              aria-label={t("workspace.surveys.summary.more_actions")}>
              <MoreHorizontalIcon />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="min-w-48">
            {visibleIconActions.map((action) => (
              <DropdownMenuItem
                key={action.tooltip}
                className="lg:hidden"
                disabled={action.disabled}
                icon={<action.icon className="size-4" />}
                onClick={action.onClick}>
                {action.tooltip}
              </DropdownMenuItem>
            ))}
            {canReset && visibleIconActions.length > 0 && <DropdownMenuSeparator className="lg:hidden" />}
            {canReset && (
              <DropdownMenuItem
                className="text-destructive focus:text-destructive"
                icon={<ListRestart className="size-4" />}
                onClick={() => setIsResetModalOpen(true)}>
                {t("workspace.surveys.summary.reset_survey")}
              </DropdownMenuItem>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      )}
      {!survey.archivedAt && (
        <Button onClick={() => setIsShareOpen(true)}>{t("workspace.surveys.summary.share_survey")}</Button>
      )}

      {!survey.archivedAt && (
        <ShareSurveyModal
          survey={survey}
          workspace={workspace}
          publicDomain={publicDomain}
          open={isStartView || isShareOpen}
          setOpen={(open) => {
            if (open) return;
            if (isStartView) setSearch({ share: undefined }, false);
            setIsShareOpen(false);
          }}
          user={user}
          modalView={isStartView ? "start" : "share"}
          isReadOnly={isReadOnly}
        />
      )}
      <SuccessMessage survey={survey} workspace={workspace} search={search} setSearch={setSearch} />

      {responseCount > 0 && (
        <EditPublicSurveyAlertDialog
          open={isCautionDialogOpen}
          setOpen={setIsCautionDialogOpen}
          isLoading={isDuplicating}
          primaryButtonAction={async () => {
            setIsCautionDialogOpen(false);
            await navigate({ href: editHref });
          }}
          primaryButtonText={t("common.edit")}
          secondaryButtonAction={() => void duplicateSurveyAndRoute()}
          secondaryButtonText={t("workspace.surveys.edit.caution_edit_duplicate")}
        />
      )}

      <ConfirmationModal
        open={isResetModalOpen}
        setOpen={setIsResetModalOpen}
        title={t("workspace.surveys.summary.delete_all_existing_responses_and_displays")}
        body={t("workspace.surveys.summary.reset_survey_warning_count", { count: responseCount })}
        buttonText={t("workspace.surveys.summary.reset_survey")}
        onConfirm={() => void handleResetSurvey()}
        buttonVariant="destructive"
        buttonLoading={isResetting}
      />
    </div>
  );
};
