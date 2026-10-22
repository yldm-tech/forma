import { useMutation, useQueryClient } from "@tanstack/react-query";
import { type SyntheticEvent, useCallback, useEffect, useMemo, useReducer, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../../lib/api";
import { surveyKeys } from "../list/query";
import { getAiErrorCode, getAiErrorMessage } from "./error-messages";
import type { GeneratedSurveyPayload, GenerationDraftSnapshot } from "./events";
import { INITIAL_AI_CREATE_STATE, aiCreateReducer } from "./machine";
import { streamSurveyGeneration } from "./stream-client";
import { AI_SURVEY_PROMPT_MIN_LENGTH } from "./utils";

/** forma2's createV3Survey(payload, "ai"): the generated payload, created as it is. */
export const createGeneratedSurvey = (payload: GeneratedSurveyPayload) =>
  api.post<{ data: { id: string } }>("/api/v3/surveys?createdFrom=ai", payload).then((res) => res.data);

/**
 * forma2's useCreateSurveyWithAI: the prompt, the languages, the generation stream folded into the draft machine, and the write behind "Open in editor". The prompt lives outside the reducer, so it survives a failed generation.
 */
export const useCreateSurveyWithAI = ({
  workspaceId,
  defaultLanguage,
  surveyType,
  isAIAvailable,
  onSuccess,
}: {
  workspaceId: string;
  /** Seeds the language picker. */
  defaultLanguage: string;
  /** The type the workspace's channel implies, so the draft matches what "Start from scratch" would build. */
  surveyType: "link" | "app";
  isAIAvailable: boolean;
  onSuccess: (surveyId: string) => void;
}) => {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [prompt, setPrompt] = useState("");
  // Ordered: the first is the survey's default language and the one the model writes in; the rest are translated in.
  const [languages, setLanguages] = useState<string[]>([defaultLanguage]);
  const [state, dispatch] = useReducer(aiCreateReducer, INITIAL_AI_CREATE_STATE);
  const [isNavigatingToEditor, setIsNavigatingToEditor] = useState(false);
  const abortControllerRef = useRef<AbortController | null>(null);

  /** Anything a reload would destroy: a generation in flight, the write behind "Open in editor", and a finished draft nobody has opened. */
  const hasUnsavedWork =
    state.status === "generating" || state.status === "creating" || state.payload !== null;
  const hasUnsavedWorkRef = useRef(hasUnsavedWork);
  useEffect(() => {
    hasUnsavedWorkRef.current = hasUnsavedWork;
  });

  useEffect(() => {
    const onBeforeUnload = (event: BeforeUnloadEvent) => {
      if (!hasUnsavedWorkRef.current) return;
      event.preventDefault();
    };
    globalThis.addEventListener("beforeunload", onBeforeUnload);
    return () => globalThis.removeEventListener("beforeunload", onBeforeUnload);
  }, []);

  // Snapshots land far faster than the screen can usefully change: the newest one is buffered and dispatched at most once per frame.
  const pendingSnapshotRef = useRef<GenerationDraftSnapshot | null>(null);
  const frameRef = useRef<number | null>(null);

  /** Drops whatever the previous generation had queued, so a snapshot buffered for the next frame cannot land in a new run. */
  const discardQueuedSnapshot = useCallback(() => {
    pendingSnapshotRef.current = null;
    if (frameRef.current !== null) {
      globalThis.cancelAnimationFrame(frameRef.current);
      frameRef.current = null;
    }
  }, []);

  const flushSnapshot = useCallback(() => {
    frameRef.current = null;
    const snapshot = pendingSnapshotRef.current;
    pendingSnapshotRef.current = null;
    if (snapshot) dispatch({ type: "SNAPSHOT", snapshot });
  }, []);

  const queueSnapshot = useCallback(
    (snapshot: GenerationDraftSnapshot) => {
      pendingSnapshotRef.current = snapshot;
      frameRef.current ??= globalThis.requestAnimationFrame(flushSnapshot);
    },
    [flushSnapshot]
  );

  useEffect(
    () => () => {
      abortControllerRef.current?.abort();
      if (frameRef.current !== null) globalThis.cancelAnimationFrame(frameRef.current);
    },
    []
  );

  const createSurvey = useMutation({
    mutationFn: createGeneratedSurvey,
    onSuccess: (survey) => {
      void queryClient.invalidateQueries({ queryKey: surveyKeys.lists() });
      setIsNavigatingToEditor(true);
      onSuccess(survey.id);
    },
    onError: (error) => dispatch({ type: "CREATE_FAILED", errorCode: getAiErrorCode(error) }),
  });

  const runGeneration = useCallback(async () => {
    const controller = new AbortController();
    abortControllerRef.current = controller;
    try {
      await streamSurveyGeneration(
        { workspaceId, prompt: prompt.trim(), type: surveyType, languages },
        {
          signal: controller.signal,
          onEvent: (event) => {
            // Nothing from a run the user already abandoned reaches the reducer.
            if (abortControllerRef.current !== controller) return;
            switch (event.type) {
              case "partial":
                queueSnapshot(event.draft);
                break;
              case "done":
                flushSnapshot();
                dispatch({ type: "DONE", payload: event.payload });
                break;
              case "error":
                dispatch({ type: "FAIL", errorCode: event.code });
                break;
              default:
                break;
            }
          },
        }
      );
    } catch (error) {
      // Stop aborts the request; that is the user getting what they asked for, not a failure.
      if (controller.signal.aborted) return;
      dispatch({ type: "FAIL", errorCode: getAiErrorCode(error) });
    } finally {
      if (abortControllerRef.current === controller) abortControllerRef.current = null;
    }
  }, [flushSnapshot, languages, prompt, queueSnapshot, surveyType, workspaceId]);

  const hasUsablePrompt = isAIAvailable && prompt.trim().length >= AI_SURVEY_PROMPT_MIN_LENGTH;
  const canCreate = hasUsablePrompt && state.status === "idle";

  const handleGenerate = useCallback(
    (event: SyntheticEvent<HTMLFormElement>) => {
      event.preventDefault();
      if (!canCreate) return;
      dispatch({ type: "SUBMIT", prompt: prompt.trim() });
      void runGeneration();
    },
    [canCreate, prompt, runGeneration]
  );

  const handleStop = useCallback(() => {
    abortControllerRef.current?.abort();
    discardQueuedSnapshot();
    dispatch({ type: "STOP" });
  }, [discardQueuedSnapshot]);

  const handleRegenerate = useCallback(() => {
    // Reachable with a prompt the form would have refused: edit it, clear it, go back to the kept draft.
    if (!hasUsablePrompt) return;
    discardQueuedSnapshot();
    dispatch({ type: "REGENERATE", prompt: prompt.trim() });
    void runGeneration();
  }, [discardQueuedSnapshot, hasUsablePrompt, prompt, runGeneration]);

  const handleEditPrompt = useCallback(() => {
    abortControllerRef.current?.abort();
    discardQueuedSnapshot();
    dispatch({ type: "EDIT_PROMPT" });
  }, [discardQueuedSnapshot]);

  const handleOpenInEditor = useCallback(() => {
    if (state.status !== "review" || !state.payload) return;
    dispatch({ type: "CREATE" });
    createSurvey.mutate(state.payload);
  }, [createSurvey, state.payload, state.status]);

  const handleBackToDraft = useCallback(() => dispatch({ type: "BACK_TO_DRAFT" }), []);
  const clearError = useCallback(() => dispatch({ type: "CLEAR_ERROR" }), []);

  const errorMessage = useMemo(
    () => (state.errorCode === null ? null : getAiErrorMessage(state.errorCode, t)),
    [state.errorCode, t]
  );

  /** The status line lists only phases that were reached: until the model writes its first JSON there is nothing real to report. */
  const statusMessage = useMemo(() => {
    if (state.draft.questions.length > 0) {
      return t("workspace.surveys.ai_create.status_writing_questions", {
        count: state.draft.questions.length,
      });
    }
    if (state.draft.name) return t("workspace.surveys.ai_create.status_writing_title");
    return t("workspace.surveys.ai_create.status_planning");
  }, [state.draft.name, state.draft.questions.length, t]);

  return {
    prompt,
    setPrompt,
    languages,
    setLanguages,
    status: state.status,
    draft: state.draft,
    /** The prompt the draft on screen came from, not always the one in the textarea. */
    submittedPrompt: state.submittedPrompt,
    canCreate,
    errorMessage,
    statusMessage,
    isCreatingSurvey: state.status === "creating" || isNavigatingToEditor,
    handleGenerate,
    handleStop,
    handleRegenerate,
    handleEditPrompt,
    handleBackToDraft,
    handleOpenInEditor,
    clearError,
    /** A finished draft the user stepped away from and can still return to. */
    hasKeptDraft: state.payload !== null && state.status === "idle",
    hasUnsavedWork,
  };
};
