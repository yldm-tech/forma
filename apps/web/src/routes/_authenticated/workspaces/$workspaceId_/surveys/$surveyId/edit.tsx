import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { ZUserLocale } from "@forma/types/user";
import { createFileRoute, notFound } from "@tanstack/react-router";
import { RouteError, RoutePending } from "../../../../../../components/shell/route-states";
import { ErrorComponent } from "../../../../../../components/ui/error-component";
import { actionClassesQuery } from "../../../../../../features/action-classes/api";
import { segmentsQuery } from "../../../../../../features/segments/api";
import {
  contactAttributeKeysQuery,
  editorSurveyQuery,
  followUpTeamMembersQuery,
  quotasQuery,
  responseCountsQuery,
  toEditorWorkspace,
} from "../../../../../../features/survey-editor/api";
import { SurveyEditor } from "../../../../../../features/survey-editor/components/survey-editor";
import { SURVEY_BG_COLORS } from "../../../../../../features/survey-editor/lib/constants";
import { resolveSurveySchedulingConfig } from "../../../../../../features/survey-editor/lib/scheduling-config";
import { authOptionsQuery, meQuery } from "../../../../../../lib/queries/auth";
import { accessFlags } from "../../../../../../lib/roles";
import { workspaceQuery } from "../../../../../../lib/queries/workspaces";
import { guardPageError } from "../../../../../../lib/route-guards";

interface EditorSearch {
  /** forma2's ?mode=cx: the reduced editor of the CX templates, without languages and follow-ups. */
  mode?: "cx";
}

// The survey editor, outside the workspace's app shell (forma2's (survey-editor) route group), so it fills the window.
export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId_/surveys/$surveyId/edit")({
  validateSearch: (search: Record<string, unknown>): EditorSearch =>
    search.mode === "cx" ? { mode: "cx" } : {},
  loader: async ({ context: { queryClient }, params }) => {
    const [, survey] = await Promise.all([
      queryClient.ensureQueryData(workspaceQuery(params.workspaceId)),
      queryClient.ensureQueryData(editorSurveyQuery(params.surveyId)),
    ]).catch(guardPageError);
    // forma2 answered a survey opened under another workspace's URL with a not-found.
    if (survey.workspaceId !== params.workspaceId) throw notFound();
    await Promise.all([
      queryClient.ensureQueryData(actionClassesQuery(params.workspaceId)),
      queryClient.ensureQueryData(contactAttributeKeysQuery(params.workspaceId)),
      queryClient.ensureQueryData(segmentsQuery(params.workspaceId)),
      queryClient.ensureQueryData(quotasQuery(params.surveyId)),
      queryClient.ensureQueryData(responseCountsQuery(params.workspaceId, params.surveyId, survey.name)),
      queryClient.ensureQueryData(authOptionsQuery),
    ]).catch(guardPageError);
  },
  pendingComponent: RoutePending,
  errorComponent: RouteError,
  component: SurveyEditorPage,
});

function SurveyEditorPage() {
  const { workspaceId, surveyId } = Route.useParams();
  const { mode } = Route.useSearch();
  const { data: me } = useSuspenseQuery(meQuery);
  const { data: authOptions } = useSuspenseQuery(authOptionsQuery);
  const { data: workspaceDetail } = useSuspenseQuery(workspaceQuery(workspaceId));
  const { data: survey } = useSuspenseQuery(editorSurveyQuery(surveyId));
  const { data: actionClasses } = useSuspenseQuery(actionClassesQuery(workspaceId));
  const { data: contactAttributeKeys } = useSuspenseQuery(contactAttributeKeysQuery(workspaceId));
  const { data: segments } = useSuspenseQuery(segmentsQuery(workspaceId));
  const { data: quotas } = useSuspenseQuery(quotasQuery(surveyId));
  const { data: responseCounts } = useSuspenseQuery(responseCountsQuery(workspaceId, surveyId, survey.name));
  // Only a survey that has follow-ups shows the tab that lists their recipients.
  const { data: teamMemberDetails = [] } = useQuery({
    ...followUpTeamMembersQuery(workspaceId, workspaceDetail.organizationId),
    enabled: survey.followUps.length > 0,
  });

  // forma2 refused the editor to a member whose teams only grant read access to the workspace.
  if (workspaceDetail.access.isReadOnly) {
    return (
      <div className="p-6">
        <ErrorComponent />
      </div>
    );
  }

  return (
    <div className="h-dvh">
      <SurveyEditor
        survey={survey}
        workspace={toEditorWorkspace(workspaceDetail)}
        actionClasses={actionClasses}
        contactAttributeKeys={contactAttributeKeys}
        segments={segments}
        responseCount={responseCounts.responseCount}
        finishedResponseCount={responseCounts.finishedResponseCount}
        isReadOnly={workspaceDetail.access.isReadOnly}
        colors={SURVEY_BG_COLORS}
        // Offered only when the instance has reCAPTCHA keys: without them the server refuses a survey that turns it on.
        isSpamProtectionAllowed={authOptions.spamProtectionAvailable}
        isCxMode={mode === "cx"}
        surveySchedulingConfig={resolveSurveySchedulingConfig(workspaceDetail.surveyScheduling)}
        locale={ZUserLocale.safeParse(me.locale).data ?? "en-US"}
        userEmail={me.email}
        teamMemberDetails={teamMemberDetails}
        quotas={quotas}
        canSaveAsTemplate={accessFlags(workspaceDetail.access.role).isOwnerOrManager}
      />
    </div>
  );
}
