import { useAtomValue, useSetAtom, useStore } from "jotai";
import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ApiError } from "../../../lib/api";
import { toast } from "../../../lib/toast";
import {
  MUTATION_TIMEOUT_MS,
  type TPatchWorkflowInput,
  type TWorkflowResource,
  classifyWorkflowSaveError,
  getWorkflowApiErrorMessage,
  workflowsApi,
} from "../api";
import { type TWorkflowDefinition, ZWorkflowDefinition } from "../definition";
import { workflowDefinitionToFlowNodes } from "../lib/definition-to-flow";
import {
  hydrateWorkflowEditorAtom,
  isWorkflowDirtyAtom,
  isWorkflowSavingAtom,
  isWorkflowTransitioningAtom,
  markWorkflowDraftSavedAtom,
  setWorkflowAtom,
  setWorkflowSaveErrorAtom,
  setWorkflowSavingAtom,
  setWorkflowTransitioningAtom,
  workflowAtom,
  workflowDefinitionAtom,
  workflowDraftSignatureAtom,
  workflowEditorAtom,
  workflowSaveErrorAtom,
} from "../state/editor";

interface UseWorkflowBuilderArgs {
  workflowId: string;
  isReadOnly: boolean;
  /** The URL's workspace: a workflow loaded under another workspace's URL is refused, since the API authorizes by the workflow's own workspace. */
  workspaceId?: string;
  /** The builder page owns the load and the autosave; components that only need the actions and the editor state (the header, the title) pass false. */
  loadOnMount?: boolean;
}

// Long enough to batch a typing burst into one PATCH, short enough that edits reach the server before the user reaches for a lifecycle action or leaves.
export const WORKFLOW_AUTOSAVE_DELAY_MS = 2000;

/** Why a draft cannot be sent at all; nothing was attempted, unlike a save the API refused. */
type TInvalidWorkflowDraft = { code: "name_required" | "definition"; detail?: string };

/** The PATCH body for the draft, or why it cannot be sent. The definition is left out while the workflow is enabled, which the API would refuse with a 422. */
const buildWorkflowPatch = (state: {
  workflow: TWorkflowResource;
  workflowName: string;
  workflowDescription: string;
  definition: TWorkflowDefinition;
}):
  | { patch: TPatchWorkflowInput; trimmedName: string; trimmedDescription: string | null }
  | { invalid: TInvalidWorkflowDraft } => {
  const trimmedName = state.workflowName.trim();
  if (!trimmedName) return { invalid: { code: "name_required" } };
  const trimmedDescription = state.workflowDescription.trim() || null;
  const patch: TPatchWorkflowInput = { name: trimmedName, description: trimmedDescription };
  if (state.workflow.status !== "enabled") {
    const parsedDefinition = ZWorkflowDefinition.safeParse(state.definition);
    if (!parsedDefinition.success) {
      return { invalid: { code: "definition", detail: parsedDefinition.error.issues[0]?.message } };
    }
    patch.definition = parsedDefinition.data;
  }
  return { patch, trimmedName, trimmedDescription };
};

const describeInvalidDraft = (invalid: TInvalidWorkflowDraft, t: (key: string) => string): string => {
  if (invalid.code === "name_required") return t("workspace.workflows.name_required");
  return invalid.detail ?? t("workspace.workflows.validation_failed");
};

/** The reason a save was refused, for the header pill's tooltip: the field-level reason the API gives in invalid_params where there is one, else its detail. A 5xx or no answer has no reason worth quoting. */
const describeSaveErrorDetail = (error: unknown, t: (key: string) => string): string | null => {
  if (!(error instanceof ApiError) || error.status >= 500) return null;
  const param = error.invalidParams[0];
  if (param?.name === "name") return t("workspace.workflows.name_invalid");
  if (param) return param.reason;
  return error.message.trim() ? error.message : null;
};

type TLifecycleOperation = "enable" | "disable" | "archive" | "unarchive";

/** forma2's useWorkflowBuilder: loads the workflow into the editor, autosaves the draft two seconds after the last edit, flushes it on unmount, and runs the lifecycle transitions after flushing pending edits. */
export const useWorkflowBuilder = ({
  workspaceId,
  workflowId,
  isReadOnly,
  loadOnMount = true,
}: UseWorkflowBuilderArgs) => {
  const { t } = useTranslation();
  const store = useStore();
  const workflow = useAtomValue(workflowAtom);
  const definition = useAtomValue(workflowDefinitionAtom);
  const isDirty = useAtomValue(isWorkflowDirtyAtom);
  const hydrateEditor = useSetAtom(hydrateWorkflowEditorAtom);
  const setWorkflow = useSetAtom(setWorkflowAtom);
  const markDraftSaved = useSetAtom(markWorkflowDraftSavedAtom);
  const isSaving = useAtomValue(isWorkflowSavingAtom);
  const isTransitioning = useAtomValue(isWorkflowTransitioningAtom);
  const setIsSaving = useSetAtom(setWorkflowSavingAtom);
  const setIsTransitioning = useSetAtom(setWorkflowTransitioningAtom);
  // Subscribed, not read through the store: clearing the error has to re-run the autosave effect, which is how the online listener re-arms a retry.
  const saveError = useAtomValue(workflowSaveErrorAtom);
  const draftSignature = useAtomValue(workflowDraftSignatureAtom);
  const setSaveError = useSetAtom(setWorkflowSaveErrorAtom);

  const [isFetching, setIsFetching] = useState(loadOnMount);
  const [loadError, setLoadError] = useState<string | null>(null);
  // Loading means nothing correct to render yet: remounting for the same workflow (the editor and runs tabs) keeps the editor on screen.
  const isLoading = isFetching && workflow?.id !== workflowId;

  useEffect(() => {
    if (!loadOnMount) return;
    const controller = new AbortController();
    setIsFetching(true);
    setLoadError(null);

    // Whether the editor already holds unsaved edits for this workflow; re-read at each use, since a draft can be saved or edited while the fetch is in flight.
    const holdsUnsavedDraft = () =>
      store.get(workflowEditorAtom).workflow?.id === workflowId && store.get(isWorkflowDirtyAtom);

    workflowsApi
      .get(workflowId, controller.signal)
      .then((loadedWorkflow) => {
        if (controller.signal.aborted) return;
        if (workspaceId && loadedWorkflow.workspaceId !== workspaceId) {
          const message = t("workspace.workflows.load_failed");
          setLoadError(message);
          toast.error(message);
          return;
        }
        // Hydrating over an unsaved draft would destroy it and make it read clean; refresh only the server snapshot and let the autosave retry the draft.
        if (holdsUnsavedDraft()) {
          setWorkflow(loadedWorkflow);
          return;
        }
        hydrateEditor({
          workflow: loadedWorkflow,
          flowNodes: workflowDefinitionToFlowNodes(loadedWorkflow.definition, t),
          isReadOnly,
        });
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) return;
        const message = getWorkflowApiErrorMessage(error, t("workspace.workflows.load_failed"));
        setLoadError(message);
        toast.error(message);
      })
      .finally(() => {
        if (controller.signal.aborted) return;
        setIsFetching(false);
      });

    return () => controller.abort();
  }, [workspaceId, workflowId, hydrateEditor, setWorkflow, store, t, loadOnMount, isReadOnly]);

  const isArchived = workflow?.status === "archived";
  const isEnabled = workflow?.status === "enabled";
  // The API refuses definition edits while enabled or archived; the name and description stay editable while enabled.
  const canEditDefinition = Boolean(workflow && !isReadOnly && !isEnabled && !isArchived);
  const canEditMetadata = Boolean(workflow && !isReadOnly && !isArchived);

  // Reads the editor through the store, so a caller can save right after an atom write without a stale closure dropping the edit. silent is the autosave mode: an unsendable draft is skipped quietly (the editor already shows why) and success does not toast. Resolves true only when the draft was persisted.
  const save = useCallback(
    async ({ silent = false }: { silent?: boolean } = {}): Promise<boolean> => {
      const state = store.get(workflowEditorAtom);
      const currentWorkflow = state.workflow;
      const currentDefinition = state.definition;
      if (!currentWorkflow || !currentDefinition) return false;

      // Built before the overlap guard so an explicit save always explains why it did nothing.
      const built = buildWorkflowPatch({
        workflow: currentWorkflow,
        workflowName: state.workflowName,
        workflowDescription: state.workflowDescription,
        definition: currentDefinition,
      });
      // Not a failed save: nothing was attempted, so no saveError is recorded.
      if ("invalid" in built) {
        if (!silent) toast.error(describeInvalidDraft(built.invalid, t));
        return false;
      }
      const { patch, trimmedName, trimmedDescription } = built;

      // A save landing during a transition can clobber the transitioned status, and the other way round.
      if (store.get(isWorkflowSavingAtom) || store.get(isWorkflowTransitioningAtom)) return false;

      // Read before the await, so a failure records the draft that was sent rather than what the user has typed since.
      const attemptedSignature = store.get(workflowDraftSignatureAtom);

      setIsSaving(true);
      try {
        const savedWorkflow = await workflowsApi.update(currentWorkflow.id, patch);
        setWorkflow(savedWorkflow);
        // The editor state as sent, not the parsed payload: zod's normalization would make the snapshot never equal the state it came from, leaving the editor permanently dirty.
        markDraftSaved({
          workflowName: trimmedName,
          workflowDescription: trimmedDescription ?? "",
          definition: currentDefinition,
        });
        if (!silent) toast.success(t("workspace.workflows.save_success"));
        return true;
      } catch (error) {
        // Persistent state rather than a toast that fades: the header pill reads it and stays failed until a save lands. It is also the autosave's no-retry guard.
        setSaveError({
          draftSignature: attemptedSignature,
          kind: classifyWorkflowSaveError(error),
          detail: describeSaveErrorDetail(error, t),
        });
        if (!silent) toast.error(getWorkflowApiErrorMessage(error, t("workspace.workflows.save_failed")));
        return false;
      } finally {
        setIsSaving(false);
      }
    },
    [store, setWorkflow, markDraftSaved, setIsSaving, setSaveError, t]
  );

  // Autosave: the page-level instance persists a dirty draft shortly after the user stops editing. The draft signature in the dependencies resets the timer on each edit, and isSaving re-arms it when edits piled up during a save.
  useEffect(() => {
    if (!loadOnMount || isReadOnly) return;
    if (!isDirty || isSaving || isTransitioning) return;
    if (!workflow || workflow.status === "archived") return;
    // Never re-send a draft the server refused, or a persistent failure would PATCH once per debounce window forever.
    if (saveError?.draftSignature === draftSignature) return;
    const timeoutHandle = setTimeout(() => void save({ silent: true }), WORKFLOW_AUTOSAVE_DELAY_MS);
    return () => clearTimeout(timeoutHandle);
  }, [
    loadOnMount,
    isReadOnly,
    isDirty,
    isSaving,
    isTransitioning,
    workflow,
    draftSignature,
    saveError,
    save,
  ]);

  // Reconnecting is the one event worth retrying by itself: clearing an "unreachable" error re-arms the debounced effect above.
  useEffect(() => {
    if (!loadOnMount || isReadOnly) return;
    const handleOnline = () => {
      if (store.get(workflowSaveErrorAtom)?.kind !== "unreachable") return;
      setSaveError(null);
    };
    window.addEventListener("online", handleOnline);
    return () => window.removeEventListener("online", handleOnline);
  }, [loadOnMount, isReadOnly, store, setSaveError]);

  // Flush on unmount: the newest edits may still be inside the debounce window when the user navigates away. Routed through a ref, refreshed after every commit, so the cleanup runs at unmount only.
  const flushOnUnmountRef = useRef<() => void>(() => undefined);
  useEffect(() => {
    flushOnUnmountRef.current = () => {
      if (!loadOnMount || isReadOnly) return;
      const state = store.get(workflowEditorAtom);
      if (!state.workflow || state.workflow.status === "archived") return;
      if (!store.get(isWorkflowDirtyAtom)) return;
      if (!state.isSaving && !state.isTransitioning) {
        void save({ silent: true });
        return;
      }
      // A write is in flight carrying the snapshot from when it started; wait for it to settle, then send what is still unsaved. The store belongs to the surrounding layout, so it outlives this page.
      const unsubscribe = store.sub(workflowEditorAtom, () => {
        const current = store.get(workflowEditorAtom);
        if (current.isSaving || current.isTransitioning) return;
        unsubscribe();
        if (!store.get(isWorkflowDirtyAtom)) return;
        void save({ silent: true });
      });
      // A write that never settles must not pin the subscription for the rest of the session.
      setTimeout(unsubscribe, MUTATION_TIMEOUT_MS + WORKFLOW_AUTOSAVE_DELAY_MS);
    };
  });
  useEffect(() => () => flushOnUnmountRef.current(), []);

  // A reload or a closed tab kills the flush above, so the browser's own confirm is the only safety net for unsaved edits.
  useEffect(() => {
    if (!loadOnMount || isReadOnly) return;
    const handleBeforeUnload = (event: BeforeUnloadEvent) => {
      if (!store.get(isWorkflowDirtyAtom)) return;
      event.preventDefault();
    };
    window.addEventListener("beforeunload", handleBeforeUnload);
    return () => window.removeEventListener("beforeunload", handleBeforeUnload);
  }, [loadOnMount, isReadOnly, store]);

  const transition = useCallback(
    async (operation: TLifecycleOperation) => {
      if (!workflow) return;
      // The server refuses every transition from a read-only member; never send one, nor flush a save on their behalf.
      if (isReadOnly) return;
      // Overlapping writes race, and the last answer to land would desync the shown status from the server.
      if (store.get(isWorkflowSavingAtom) || store.get(isWorkflowTransitioningAtom)) return;

      // Flush first, so the transition acts on what the user sees: enable validates the persisted definition, not the local draft.
      if (store.get(isWorkflowDirtyAtom)) {
        const flushed = await save({ silent: true });
        if (!flushed && operation === "enable") {
          toast.error(t("workspace.workflows.enable_blocked_unsaved_changes"));
          return;
        }
      }

      const lifecycleOps = {
        enable: {
          run: workflowsApi.enable,
          success: () => t("workspace.workflows.enable_success"),
          failure: () => t("workspace.workflows.enable_failed"),
        },
        disable: {
          run: workflowsApi.disable,
          success: () => t("workspace.workflows.disable_success"),
          failure: () => t("workspace.workflows.disable_failed"),
        },
        archive: {
          run: workflowsApi.archive,
          success: () => t("workspace.workflows.archive_success"),
          failure: () => t("workspace.workflows.archive_failed"),
        },
        unarchive: {
          run: workflowsApi.unarchive,
          success: () => t("workspace.workflows.unarchive_success"),
          failure: () => t("workspace.workflows.unarchive_failed"),
        },
      } as const;
      const op = lifecycleOps[operation];

      setIsTransitioning(true);
      try {
        setWorkflow(await op.run(workflow.id));
        toast.success(op.success());
      } catch (error) {
        toast.error(getWorkflowApiErrorMessage(error, op.failure()));
      } finally {
        setIsTransitioning(false);
      }
    },
    [store, workflow, setWorkflow, setIsTransitioning, save, t, isReadOnly]
  );

  return {
    workflow,
    definition,
    isLoading,
    loadError,
    isSaving,
    isDirty,
    isTransitioning,
    canEditDefinition,
    canEditMetadata,
    isArchived,
    isEnabled,
    save,
    enable: () => transition("enable"),
    disable: () => transition("disable"),
    archive: () => transition("archive"),
    unarchive: () => transition("unarchive"),
  };
};
