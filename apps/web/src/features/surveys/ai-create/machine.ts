import { type AiDraftState, EMPTY_AI_DRAFT, mergeAiDraftSnapshot } from "./draft-reducer";
import type { GeneratedSurveyPayload, GenerationDraftSnapshot } from "./events";

export type AiCreateStatus = "idle" | "generating" | "review" | "creating";

export interface AiCreateState {
  status: AiCreateStatus;
  draft: AiDraftState;
  /** The validated create payload, only ever set from the stream's done event. */
  payload: GeneratedSurveyPayload | null;
  /** An error code, not a message, so the reducer stays free of t. */
  errorCode: string | null;
  /** The prompt that produced what is on screen, not the one in the textarea: editing the prompt keeps the finished draft. */
  submittedPrompt: string;
  /** The last finished draft, held aside while a regeneration runs, so stopping or failing it does not cost the one the user had. */
  previous: { draft: AiDraftState; payload: GeneratedSurveyPayload; submittedPrompt: string } | null;
}

export type AiCreateAction =
  | { type: "SUBMIT"; prompt: string }
  | { type: "SNAPSHOT"; snapshot: GenerationDraftSnapshot }
  | { type: "DONE"; payload: GeneratedSurveyPayload }
  | { type: "STOP" }
  | { type: "FAIL"; errorCode: string }
  | { type: "CREATE_FAILED"; errorCode: string }
  | { type: "EDIT_PROMPT" }
  | { type: "BACK_TO_DRAFT" }
  | { type: "REGENERATE"; prompt: string }
  | { type: "CREATE" }
  | { type: "CLEAR_ERROR" }
  | { type: "RESET" };

export const INITIAL_AI_CREATE_STATE: AiCreateState = {
  status: "idle",
  draft: EMPTY_AI_DRAFT,
  payload: null,
  errorCode: null,
  submittedPrompt: "",
  previous: null,
};

/** Raised locally rather than by the server: the stream succeeded but carried no question to answer. */
export const AI_NOTHING_GENERATED_CODE = "ai_nothing_generated";

/** Whether the payload carries a survey: the create body nests elements in blocks, so it can have blocks and nothing to answer. */
function isEmptyPayload(payload: GeneratedSurveyPayload): boolean {
  const blocks = Array.isArray(payload.blocks) ? (payload.blocks as unknown[]) : [];
  return !blocks.some((block) => {
    const elements = (block as { elements?: unknown } | null)?.elements;
    return Array.isArray(elements) && elements.length > 0;
  });
}

/** Puts a held-aside draft back on screen, or falls back to a clean slate. */
function restorePrevious(state: AiCreateState, errorCode: string | null = null): AiCreateState {
  if (!state.previous) return { ...INITIAL_AI_CREATE_STATE, errorCode };
  return {
    status: "review",
    draft: state.previous.draft,
    payload: state.previous.payload,
    errorCode,
    submittedPrompt: state.previous.submittedPrompt,
    previous: null,
  };
}

/** A snapshot that lands after Stop must not resurrect the generating view. */
function applySnapshot(state: AiCreateState, snapshot: GenerationDraftSnapshot): AiCreateState {
  if (state.status !== "generating") return state;
  const draft = mergeAiDraftSnapshot(state.draft, snapshot);
  return draft === state.draft ? state : { ...state, draft };
}

function applyDone(state: AiCreateState, payload: GeneratedSurveyPayload): AiCreateState {
  // A done event from a run the user already stopped would pair the restored draft with the abandoned run's payload.
  if (state.status !== "generating") return state;
  // Judged on the payload, not the preview: a provider that returns its object in one final chunk streams no partials.
  if (isEmptyPayload(payload)) return { ...INITIAL_AI_CREATE_STATE, errorCode: AI_NOTHING_GENERATED_CODE };
  return { ...state, status: "review", payload, errorCode: null, previous: null };
}

function applyStop(state: AiCreateState): AiCreateState {
  // Stopping a regeneration restores the draft it was replacing; the partial could not be saved anyway.
  if (state.previous) return restorePrevious(state);
  // A first generation keeps whatever arrived, or goes back to the prompt.
  return state.draft.questions.length > 0 ? { ...state, status: "review" } : { ...INITIAL_AI_CREATE_STATE };
}

function applyFail(state: AiCreateState, errorCode: string): AiCreateState {
  // A failure of an abandoned run must not tear down what the user went back to.
  if (state.status !== "generating") return state;
  // The half-written draft is discarded; a failed regeneration hands back the draft it was replacing.
  return restorePrevious(state, errorCode);
}

function applyEditPrompt(state: AiCreateState): AiCreateState {
  // A finished draft is kept so the user can go back to it; a half-written one is dropped.
  if (state.payload) return { ...state, status: "idle", errorCode: null };
  if (state.previous) return { ...restorePrevious(state), status: "idle" };
  return { ...INITIAL_AI_CREATE_STATE };
}

function applyRegenerate(state: AiCreateState, prompt: string): AiCreateState {
  // The visible list clears so the old one does not sit under the new stream, but is held aside rather than destroyed.
  return {
    status: "generating",
    draft: EMPTY_AI_DRAFT,
    payload: null,
    errorCode: null,
    submittedPrompt: prompt,
    previous: state.payload
      ? { draft: state.draft, payload: state.payload, submittedPrompt: state.submittedPrompt }
      : state.previous,
  };
}

/** forma2's aiCreateReducer: idle, generating, review and creating, with a kept draft that survives a regeneration that is stopped or fails. */
export function aiCreateReducer(state: AiCreateState, action: AiCreateAction): AiCreateState {
  switch (action.type) {
    case "SUBMIT":
      return {
        status: "generating",
        draft: EMPTY_AI_DRAFT,
        payload: null,
        errorCode: null,
        submittedPrompt: action.prompt,
        previous: null,
      };
    case "SNAPSHOT":
      return applySnapshot(state, action.snapshot);
    case "DONE":
      return applyDone(state, action.payload);
    case "STOP":
      return applyStop(state);
    case "FAIL":
      return applyFail(state, action.errorCode);
    case "EDIT_PROMPT":
      return applyEditPrompt(state);
    case "BACK_TO_DRAFT":
      return state.payload ? { ...state, status: "review", errorCode: null } : state;
    case "REGENERATE":
      return applyRegenerate(state, action.prompt);
    case "CREATE":
      return state.status === "review" && state.payload
        ? { ...state, status: "creating", errorCode: null }
        : state;
    case "CREATE_FAILED":
      // The generation succeeded and the user accepted it, so a failed write costs a retry, not a regeneration.
      return { ...state, status: "review", errorCode: action.errorCode };
    case "CLEAR_ERROR":
      return state.errorCode === null ? state : { ...state, errorCode: null };
    case "RESET":
      return INITIAL_AI_CREATE_STATE;
  }
}
