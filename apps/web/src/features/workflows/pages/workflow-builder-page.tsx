import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { useSetAtom } from "jotai";
import { useEffect, useMemo } from "react";
import { useTranslation } from "react-i18next";
import type { TUserLocale } from "@forma/types/user";
import { meQuery } from "../../../lib/queries/auth";
import { workflowKeys, workflowsApi } from "../api";
import { WorkflowCanvas } from "../components/canvas/workflow-canvas";
import { WorkflowInspectorPanel } from "../components/inspector/workflow-inspector-panel";
import { WorkflowEmailAuthoringProvider } from "../components/workflow-email-authoring-context";
import { WorkflowBuilderBodyLoading } from "../components/workflows-loading";
import { useReconcileTriggerEndingCards } from "../hooks/use-reconcile-trigger-ending-cards";
import { useWorkflowSurvey, useWorkflowSurveyOptions } from "../hooks/use-trigger-survey-picker";
import { useWorkflowBuilder } from "../hooks/use-workflow-builder";
import { useTrackWorkflowSurface } from "../hooks/use-track-workflow-surface";
import { useWorkflowNodeUrlSync } from "../hooks/use-workflow-node-url-sync";
import { type TWorkflowEmailAuthoringContext, resolveBoundTriggerSurvey } from "../lib/authoring-context";
import { hasBoundTriggerSurveyAtom } from "../state/editor";

interface WorkflowBuilderPageProps {
  workspaceId: string;
  workflowId: string;
  isReadOnly: boolean;
  nodeParam: string | null;
  setNodeParam: (nodeId: string | null) => void;
}

/** forma2's WorkflowBuilderPage: the canvas and the node inspector over the loaded workflow, with the email authoring context (the trigger's survey, the addressable members, the sender) the send_email form needs. */
export const WorkflowBuilderPage = ({
  workspaceId,
  workflowId,
  isReadOnly,
  nodeParam,
  setNodeParam,
}: Readonly<WorkflowBuilderPageProps>) => {
  const { t } = useTranslation();
  const builder = useWorkflowBuilder({ workspaceId, workflowId, isReadOnly });
  const setHasBoundTriggerSurvey = useSetAtom(hasBoundTriggerSurveyAtom);
  const surveyOptionsQuery = useWorkflowSurveyOptions(workspaceId);
  const { data: me } = useSuspenseQuery(meQuery);
  const { data: authoring } = useQuery({
    queryKey: workflowKeys.authoringContext(workspaceId),
    queryFn: ({ signal }) => workflowsApi.authoringContext(workspaceId, signal),
  });

  const definition = builder.definition;
  const triggerSurveyId = definition?.trigger?.config.surveyId ?? null;
  // The trigger's survey, when it is one of this workspace's: forma2 resolved it on the server at page load, here it follows the trigger as the user picks.
  const { survey } = useWorkflowSurvey(triggerSurveyId, workspaceId);

  const emailAuthoringContext = useMemo<TWorkflowEmailAuthoringContext>(
    () => ({
      survey,
      teamMemberDetails: authoring?.teamMemberDetails ?? [],
      userEmail: me.email,
      mailFrom: authoring?.mailFrom || "noreply@forma.yldm.ai",
      locale: me.locale as TUserLocale,
    }),
    [survey, authoring, me.email, me.locale]
  );

  // Whether the trigger's survey resolves, pushed into the atom the validity and the canvas read. The resolved survey is authoritative (it catches deleted surveys); membership in the picker's list vouches for a just-picked id before its survey has loaded. Keyed on the inputs, since hydration resets the atom to its optimistic default.
  const surveyOptions = surveyOptionsQuery.options;
  useEffect(() => {
    const isBound =
      Boolean(resolveBoundTriggerSurvey(emailAuthoringContext, definition)) ||
      (triggerSurveyId !== null && surveyOptions.some((option) => option.id === triggerSurveyId));
    setHasBoundTriggerSurvey(isBound);
  }, [emailAuthoringContext, definition, triggerSurveyId, surveyOptions, setHasBoundTriggerSurvey]);

  // Prunes ending ids of deleted endings here rather than in the trigger form, so the canvas and the enable gate stay right without the inspector open.
  useReconcileTriggerEndingCards({ definition, isEditable: builder.canEditDefinition });

  useWorkflowNodeUrlSync({ isEnabled: Boolean(builder.workflow), nodeParam, setNodeParam });

  // The builder counts as visited once the workflow is loaded, not while the skeleton shows.
  useTrackWorkflowSurface(builder.workflow ? "builder" : null, {
    workflowId,
    workflowStatus: builder.workflow?.status,
    isReadOnly,
  });

  if (builder.isLoading) return <WorkflowBuilderBodyLoading />;

  if (!builder.workflow) {
    return (
      <div className="rounded-lg border border-slate-200 bg-white p-6 text-sm text-slate-600">
        {builder.loadError ?? t("workspace.workflows.load_failed")}
      </div>
    );
  }

  return (
    // Takes the height the layout hands down; canvas and inspector share it, the canvas clipping (it pans) and the inspector scrolling, so the page never grows.
    <div className="flex min-h-0 flex-1 flex-col gap-4">
      <WorkflowEmailAuthoringProvider value={emailAuthoringContext}>
        <section className="flex min-h-0 flex-1 gap-4">
          <WorkflowCanvas isEditable={builder.canEditDefinition} />
          <WorkflowInspectorPanel isEditingNode={builder.canEditDefinition} />
        </section>
      </WorkflowEmailAuthoringProvider>
    </div>
  );
};
