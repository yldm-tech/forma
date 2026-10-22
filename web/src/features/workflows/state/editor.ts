import { createId } from "@paralleldrive/cuid2";
import type { Node } from "@xyflow/react";
import { produce } from "immer";
import { atom } from "jotai";
import type { SetStateAction } from "react";
import type { TWorkflowResource } from "../api";
import {
  type TWorkflowDefinition,
  type TWorkflowTriggerType,
  WORKFLOW_ACTIONS,
  WORKFLOW_TRIGGERS,
  ZWorkflowExecutableDefinition,
} from "../definition";

// The workflow builder's state, forma2's modules/workflows/state/editor.ts: one editor atom holding the loaded workflow and the editable draft, the atoms derived from it, and the writes the canvas, the inspector and the save flow make.

export type TWorkflowNodeCategory = "trigger" | "flow" | "action";
export type TWorkflowNodeIcon = "trigger" | "ifElse" | "email";

export type TWorkflowNodeIssue = {
  /** "setup": a draft that is simply not configured yet (amber, guiding); "error": a live or previously live workflow that cannot run as configured (red). */
  severity: "setup" | "error";
  /** The short reason the card shows in place of its summary. */
  label: string;
};

export type TWorkflowNodeData = {
  category: TWorkflowNodeCategory;
  icon: TWorkflowNodeIcon;
  /** The concrete type (`response.completed`, `send_email`, `if_else`), never shown. */
  nodeType: string;
  title: string;
  summary: string;
  isLeaf: boolean;
  issue: TWorkflowNodeIssue | null;
};

/** A pending "take me to the field that is wrong" jump, raised by the validation problems dialog and consumed by the config form that owns nodeId. */
export type TWorkflowNodeFieldFocusRequest = {
  nodeId: string;
  /** The config key within the node's form: "to", "subject", "body", "surveyId", "endingCardIds". */
  field: string;
};

/** The editable draft exactly as last sent (or hydrated). A client-side snapshot of what was sent, not the server's answer, so server-side normalization can never make the editor look permanently dirty. */
export type TWorkflowSavedDraft = {
  workflowName: string;
  workflowDescription: string;
  definition: TWorkflowDefinition | null;
};

/** The last save the server did not accept. It stays until a save succeeds, since a silent autosave has no other channel to report through, and doubles as the autosave's no-retry guard: the effect refuses to re-send draftSignature. */
export type TWorkflowSaveError = {
  draftSignature: string;
  /** "unreachable": no answer at all (offline, a 5xx, the 15 s timeout), worth retrying once the browser is back online. "rejected": the API refused the draft. */
  kind: "unreachable" | "rejected";
  /** The API's reason, safe to show; only for "rejected". */
  detail: string | null;
};

type TWorkflowEditorState = {
  workflow: TWorkflowResource | null;
  /** The member has read-only access to the workspace. Seeded at hydrate time so the canvas atoms gate mutations on permissions, not only status. */
  isReadOnly: boolean;
  workflowName: string;
  workflowDescription: string;
  definition: TWorkflowDefinition | null;
  lastSavedDraft: TWorkflowSavedDraft | null;
  /** Epoch ms of the last successful save this session; drives the "saved" flash. */
  lastSavedAt: number | null;
  saveError: TWorkflowSaveError | null;
  selectedNodeId: string | null;
  fieldFocusRequest: TWorkflowNodeFieldFocusRequest | null;
  isInspectorCollapsed: boolean;
  isSnapToCanvasEnabled: boolean;
  isNodeConfigModalOpen: boolean;
  isSaving: boolean;
  isTransitioning: boolean;
};

const initialWorkflowEditorState: TWorkflowEditorState = {
  workflow: null,
  isReadOnly: false,
  workflowName: "",
  workflowDescription: "",
  definition: null,
  lastSavedDraft: null,
  lastSavedAt: null,
  saveError: null,
  selectedNodeId: null,
  fieldFocusRequest: null,
  isInspectorCollapsed: false,
  isSnapToCanvasEnabled: true,
  isNodeConfigModalOpen: false,
  isSaving: false,
  isTransitioning: false,
};

export const workflowEditorAtom = atom<TWorkflowEditorState>(initialWorkflowEditorState);

export const workflowAtom = atom((get) => get(workflowEditorAtom).workflow);
export const workflowNameAtom = atom((get) => get(workflowEditorAtom).workflowName);
export const workflowStatusAtom = atom((get) => get(workflowEditorAtom).workflow?.status ?? null);
export const workflowDescriptionAtom = atom((get) => get(workflowEditorAtom).workflowDescription);
export const workflowDefinitionAtom = atom((get) => get(workflowEditorAtom).definition);
export const selectedWorkflowNodeIdAtom = atom((get) => get(workflowEditorAtom).selectedNodeId);
export const workflowNodeFieldFocusRequestAtom = atom((get) => get(workflowEditorAtom).fieldFocusRequest);

// React Flow's nodes live outside the immer-produced editor state on purpose: immer freezes what it produces, and React Flow writes measured dimensions into its nodes, which throws on a frozen object.
export const workflowFlowNodesAtom = atom<Array<Node<TWorkflowNodeData>>>([]);
export const isWorkflowInspectorCollapsedAtom = atom((get) => get(workflowEditorAtom).isInspectorCollapsed);
export const isWorkflowSnapToCanvasEnabledAtom = atom((get) => get(workflowEditorAtom).isSnapToCanvasEnabled);
export const isWorkflowNodeConfigModalOpenAtom = atom((get) => get(workflowEditorAtom).isNodeConfigModalOpen);
export const isWorkflowSavingAtom = atom((get) => get(workflowEditorAtom).isSaving);
export const isWorkflowTransitioningAtom = atom((get) => get(workflowEditorAtom).isTransitioning);

// Whether the trigger's surveyId resolves to a survey of the workspace. The builder page owns it; it defaults to true so nothing flashes as broken before the page syncs it.
export const hasBoundTriggerSurveyAtom = atom<boolean>(true);

// Trigger ending ids the reconcile dropped this session. An emptied list reads as "all endings", which cannot say that the user's specific pick was emptied by deleted endings, so the trigger form keeps the "specific" scope and asks for a fresh pick.
export const prunedTriggerEndingCardIdsAtom = atom<string[]>([]);

const toSavedDraft = (state: TWorkflowEditorState): TWorkflowSavedDraft => ({
  workflowName: state.workflowName.trim(),
  workflowDescription: state.workflowDescription.trim(),
  definition: state.definition,
});

// Records the persisted draft. The save flow passes the values it sent, captured before the request, so edits made while the PATCH was in flight still read as dirty.
export const markWorkflowDraftSavedAtom = atom(null, (get, set, savedDraft: TWorkflowSavedDraft) => {
  set(
    workflowEditorAtom,
    produce(get(workflowEditorAtom), (draft) => {
      draft.lastSavedDraft = savedDraft;
      draft.lastSavedAt = Date.now();
      // Cleared in the same write as lastSavedAt, so the pill never shows the failure and the "saved" flash across two commits.
      draft.saveError = null;
    })
  );
});

export const workflowLastSavedAtAtom = atom((get) => get(workflowEditorAtom).lastSavedAt);

// Whether the draft differs from what was last persisted. Trimmed, since the save flow strips trailing whitespace anyway.
export const isWorkflowDirtyAtom = atom((get) => {
  const state = get(workflowEditorAtom);
  if (!state.workflow || !state.lastSavedDraft) return false;
  const current = toSavedDraft(state);
  return (
    current.workflowName !== state.lastSavedDraft.workflowName ||
    current.workflowDescription !== state.lastSavedDraft.workflowDescription ||
    JSON.stringify(current.definition) !== JSON.stringify(state.lastSavedDraft.definition)
  );
});

// The identity of the current draft, so the value save() records as failed and the value the autosave effect compares are the same string by construction. Untrimmed: this identifies a draft, it does not decide dirtiness.
export const workflowDraftSignatureAtom = atom((get) => {
  const state = get(workflowEditorAtom);
  return JSON.stringify({
    workflowName: state.workflowName,
    workflowDescription: state.workflowDescription,
    definition: state.definition,
  });
});

export const workflowSaveErrorAtom = atom((get) => get(workflowEditorAtom).saveError);

// The header pill's failed state: only while the draft is dirty (reverting clears it) and only for the exact draft that failed (editing past a rejection lets the pill report the current state).
export const hasWorkflowSaveFailedAtom = atom((get) => {
  const saveError = get(workflowEditorAtom).saveError;
  if (!saveError || !get(isWorkflowDirtyAtom)) return false;
  return saveError.draftSignature === get(workflowDraftSignatureAtom);
});

export type TWorkflowValidity = {
  isNameValid: boolean;
  /** The definition passes the executable schema enable and test enforce on the server. */
  isDefinitionExecutable: boolean;
  hasBoundTriggerSurvey: boolean;
  /** Hydrated with an empty problem list. Matches the server's enable checks except the ending cards one, which needs server data (see WorkflowValidationStatus); the server still rejects stale endings. */
  isReady: boolean;
};

export type TWorkflowValidationProblemCode =
  | "name_missing"
  | "trigger_missing"
  | "trigger_survey_unbound"
  | "trigger_ending_not_found"
  | "trigger_not_connected"
  | "flow_invalid"
  | "step_not_executable"
  | "step_incomplete"
  | "definition_invalid";

export type TWorkflowValidationProblem = {
  code: TWorkflowValidationProblemCode;
  /** Dotted path into the definition (or "name"), like the server's test problem fields. */
  field: string;
};

/** The field paths a problem can carry, as builders and matchers, so the producer and the problems dialog's "fix" jump cannot drift apart. */
export const workflowProblemFields = {
  name: "name",
  trigger: "trigger",
  triggerSurveyId: "trigger.config.surveyId",
  triggerEndingCardIds: "trigger.config.endingCardIds",
  /** The step-level path step_incomplete groups a send_email node's blank fields under. */
  nodeConfig: (nodeIndex: number) => `nodes.${nodeIndex}.config`,
  /** The index back out of a nodeConfig path; null for any other shape. */
  parseNodeConfigIndex: (field: string): number | null => {
    const match = /^nodes\.(\d+)\.config$/.exec(field);
    return match ? Number(match[1]) : null;
  },
} as const;

// Buckets one executable-schema issue by its path. Total: every issue maps to some code, so a failed parse always yields a problem and the list can never read "valid" while the definition is not executable.
const categorizeDefinitionIssue = (
  issuePath: ReadonlyArray<PropertyKey>,
  context: { hasTrigger: boolean; isTriggerConnected: boolean }
): TWorkflowValidationProblem => {
  const path = issuePath.map(String);
  const field = path.join(".") || "definition";
  const [root, , third] = path;

  if (root === "trigger" || root === "entryNodeId") {
    // Without a trigger both fields fail together and collapse into one "add a trigger" problem.
    return context.hasTrigger
      ? { code: "definition_invalid", field }
      : { code: "trigger_missing", field: workflowProblemFields.trigger };
  }

  if (root === "edges") {
    // Several rules report at the bare edges path; while the trigger has no outgoing edge they are all the unconnected trigger.
    if (path.length === 1 && !context.isTriggerConnected) {
      return { code: "trigger_not_connected", field };
    }
    return { code: "flow_invalid", field };
  }

  if (root === "nodes") {
    // Unreachable or duplicated nodes: the graph shape is broken.
    if (path.length === 1) return { code: "flow_invalid", field };
    // if_else nodes are stored fine but cannot run in this version of workflows.
    if (third === "type") return { code: "step_not_executable", field };
    // Missing send_email content, grouped per step so the count matches one unfinished step.
    if (third === "config") {
      return { code: "step_incomplete", field: workflowProblemFields.nodeConfig(Number(path[1])) };
    }
  }

  return { code: "definition_invalid", field };
};

export type TWorkflowValidation = {
  problems: TWorkflowValidationProblem[];
  validity: TWorkflowValidity;
};

/** The editor's live readiness rules (name, survey binding, executable schema), one parse producing both the problem list the canvas counts and the validity the Enable gate reads, so the two cannot disagree. */
export const deriveWorkflowValidation = ({
  workflowName,
  definition,
  hasBoundTriggerSurvey,
}: {
  workflowName: string;
  definition: TWorkflowDefinition | null;
  hasBoundTriggerSurvey: boolean;
}): TWorkflowValidation => {
  const problems: TWorkflowValidationProblem[] = [];

  const isNameValid = workflowName.trim().length > 0;
  if (!isNameValid) problems.push({ code: "name_missing", field: workflowProblemFields.name });

  // No definition means the editor is not hydrated: nothing to validate, and not ready.
  let isDefinitionExecutable = false;
  if (definition) {
    const hasTrigger = definition.trigger !== null;
    // The binding only matters once a trigger exists; trigger_missing already says everything otherwise.
    if (hasTrigger && !hasBoundTriggerSurvey) {
      problems.push({ code: "trigger_survey_unbound", field: workflowProblemFields.triggerSurveyId });
    }
    const parsed = ZWorkflowExecutableDefinition.safeParse(definition);
    isDefinitionExecutable = parsed.success;
    if (!parsed.success) {
      const isTriggerConnected = definition.edges.some((edge) => edge.source === definition.trigger?.id);
      for (const issue of parsed.error.issues) {
        problems.push(categorizeDefinitionIssue(issue.path, { hasTrigger, isTriggerConnected }));
      }
    }
  }

  const seen = new Set<string>();
  const dedupedProblems = problems.filter((problem) => {
    const key = `${problem.code}:${problem.field}`;
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });

  return {
    problems: dedupedProblems,
    validity: {
      isNameValid,
      isDefinitionExecutable,
      hasBoundTriggerSurvey,
      isReady: definition !== null && dedupedProblems.length === 0,
    },
  };
};

/** The readiness rule that needs server data: whether the trigger's ending cards still exist on the bound survey. One problem however many endings went stale. */
export const deriveTriggerEndingProblems = (
  endingCardIds: readonly string[],
  availableEndingIds: readonly string[]
): TWorkflowValidationProblem[] => {
  const available = new Set(availableEndingIds);
  return endingCardIds.some((endingCardId) => !available.has(endingCardId))
    ? [{ code: "trigger_ending_not_found", field: workflowProblemFields.triggerEndingCardIds }]
    : [];
};

const workflowValidationAtom = atom<TWorkflowValidation>((get) => {
  const state = get(workflowEditorAtom);
  return deriveWorkflowValidation({
    workflowName: state.workflowName,
    definition: state.definition,
    hasBoundTriggerSurvey: get(hasBoundTriggerSurveyAtom),
  });
});

export const workflowValidityAtom = atom((get) => get(workflowValidationAtom).validity);

export const workflowValidationProblemsAtom = atom((get) => get(workflowValidationAtom).problems);

// Locked is pan mode (nodes are inert), unlocked is pointer mode (select and inspect, edit when allowed). Only the user switches it.
export const isCanvasLockedAtom = atom<boolean>(false);

export const isWorkflowReadOnlyAtom = atom((get) => get(workflowEditorAtom).isReadOnly);

// The definition is editable when the status and the member's permissions both allow it: the API refuses definition edits on enabled and archived workflows, and a read-only member may not edit at all.
export const canEditWorkflowDefinitionAtom = atom((get) => {
  const state = get(workflowEditorAtom);
  if (state.isReadOnly) return false;
  const status = state.workflow?.status;
  return status === "draft" || status === "disabled";
});

// Any interactive canvas affordance (add, delete, drag, auto-layout) needs an editable definition and an unlocked canvas.
export const canMutateCanvasAtom = atom(
  (get) => get(canEditWorkflowDefinitionAtom) && !get(isCanvasLockedAtom)
);

export const setWorkflowSaveErrorAtom = atom(null, (get, set, saveError: TWorkflowSaveError | null) => {
  // A redundant clear must not produce a new state object and re-render every subscriber.
  if (get(workflowEditorAtom).saveError === saveError) return;
  set(
    workflowEditorAtom,
    produce(get(workflowEditorAtom), (draft) => {
      draft.saveError = saveError;
    })
  );
});

export const setWorkflowSavingAtom = atom(null, (get, set, isSaving: boolean) => {
  set(
    workflowEditorAtom,
    produce(get(workflowEditorAtom), (draft) => {
      draft.isSaving = isSaving;
    })
  );
});

export const setWorkflowTransitioningAtom = atom(null, (get, set, isTransitioning: boolean) => {
  set(
    workflowEditorAtom,
    produce(get(workflowEditorAtom), (draft) => {
      draft.isTransitioning = isTransitioning;
    })
  );
});

export const hydrateWorkflowEditorAtom = atom(
  null,
  (
    _get,
    set,
    {
      workflow,
      flowNodes,
      isReadOnly,
    }: {
      workflow: TWorkflowResource;
      flowNodes: Array<Node<TWorkflowNodeData>>;
      isReadOnly: boolean;
    }
  ) => {
    // Optimistic until the builder page re-syncs it; a previous workflow's state must not flash on this one.
    set(hasBoundTriggerSurveyAtom, true);
    set(prunedTriggerEndingCardIdsAtom, []);
    // Set outside produce so the nodes stay unfrozen.
    set(workflowFlowNodesAtom, flowNodes);
    set(
      workflowEditorAtom,
      produce(initialWorkflowEditorState, (draft) => {
        draft.workflow = workflow;
        draft.isReadOnly = isReadOnly;
        draft.workflowName = workflow.name;
        draft.workflowDescription = workflow.description ?? "";
        draft.definition = workflow.definition;
        // Freshly hydrated means nothing is dirty: the saved snapshot is the loaded state.
        draft.lastSavedDraft = {
          workflowName: workflow.name.trim(),
          workflowDescription: (workflow.description ?? "").trim(),
          definition: workflow.definition,
        };
        draft.selectedNodeId = workflow.definition.trigger?.id ?? null;
      })
    );
  }
);

// Replaces the server-owned snapshot only, leaving the editable draft alone so a save response or a lifecycle change that races the user's typing does not wipe it.
export const setWorkflowAtom = atom(null, (get, set, workflow: TWorkflowResource) => {
  set(
    workflowEditorAtom,
    produce(get(workflowEditorAtom), (draft) => {
      draft.workflow = workflow;
    })
  );
});

export const setWorkflowNameAtom = atom(null, (get, set, workflowName: string) => {
  set(
    workflowEditorAtom,
    produce(get(workflowEditorAtom), (draft) => {
      draft.workflowName = workflowName;
    })
  );
});

export const setWorkflowDescriptionAtom = atom(null, (get, set, workflowDescription: string) => {
  set(
    workflowEditorAtom,
    produce(get(workflowEditorAtom), (draft) => {
      draft.workflowDescription = workflowDescription;
    })
  );
});

export const setWorkflowDefinitionAtom = atom(
  null,
  (get, set, update: SetStateAction<TWorkflowDefinition | null>) => {
    const currentState = get(workflowEditorAtom);
    const nextDefinition = typeof update === "function" ? update(currentState.definition) : update;
    set(
      workflowEditorAtom,
      produce(currentState, (draft) => {
        draft.definition = nextDefinition;
      })
    );
  }
);

export const setSelectedWorkflowNodeIdAtom = atom(null, (get, set, selectedNodeId: string | null) => {
  set(
    workflowEditorAtom,
    produce(get(workflowEditorAtom), (draft) => {
      draft.selectedNodeId = selectedNodeId;
    })
  );
});

export const openWorkflowNodeConfigModalAtom = atom(null, (get, set, nodeId: string) => {
  set(
    workflowEditorAtom,
    produce(get(workflowEditorAtom), (draft) => {
      draft.selectedNodeId = nodeId;
      draft.isNodeConfigModalOpen = true;
      // Opening a node opens the inspector even if the user collapsed it, or the click looks broken.
      draft.isInspectorCollapsed = false;
      // A plain click is not a jump: drop a request no form consumed so it cannot fire later out of context.
      draft.fieldFocusRequest = null;
    })
  );
});

// A jump to one field: the same as opening the node, plus the focus request its config form consumes.
export const requestWorkflowNodeFieldFocusAtom = atom(
  null,
  (get, set, request: TWorkflowNodeFieldFocusRequest) => {
    set(
      workflowEditorAtom,
      produce(get(workflowEditorAtom), (draft) => {
        draft.selectedNodeId = request.nodeId;
        draft.isNodeConfigModalOpen = true;
        draft.isInspectorCollapsed = false;
        draft.fieldFocusRequest = request;
      })
    );
  }
);

// Cleared by the form once it has focused the control, so a later remount does not re-fire a stale jump.
export const clearWorkflowNodeFieldFocusAtom = atom(null, (get, set) => {
  if (!get(workflowEditorAtom).fieldFocusRequest) return;
  set(
    workflowEditorAtom,
    produce(get(workflowEditorAtom), (draft) => {
      draft.fieldFocusRequest = null;
    })
  );
});

export const closeWorkflowNodeConfigModalAtom = atom(null, (get, set) => {
  set(
    workflowEditorAtom,
    produce(get(workflowEditorAtom), (draft) => {
      draft.isNodeConfigModalOpen = false;
    })
  );
});

export const toggleWorkflowInspectorAtom = atom(null, (get, set) => {
  set(
    workflowEditorAtom,
    produce(get(workflowEditorAtom), (draft) => {
      draft.isInspectorCollapsed = !draft.isInspectorCollapsed;
    })
  );
});

// Drops a node and its edges, bridging each incoming edge to each outgoing one (keeping the incoming sourceHandle) so the graph stays connected. The trigger cannot be deleted.
export const deleteWorkflowNodeAtom = atom(null, (get, set, nodeId: string) => {
  set(
    workflowEditorAtom,
    produce(get(workflowEditorAtom), (draft) => {
      const definition = draft.definition;
      if (!definition) return;
      if (definition.trigger?.id === nodeId) return;

      const incoming = definition.edges.filter((edge) => edge.target === nodeId);
      const outgoing = definition.edges.filter((edge) => edge.source === nodeId);

      definition.nodes = definition.nodes.filter((node) => node.id !== nodeId);
      definition.edges = definition.edges.filter((edge) => edge.source !== nodeId && edge.target !== nodeId);

      for (const inEdge of incoming) {
        for (const outEdge of outgoing) {
          definition.edges.push({
            id: createId(),
            source: inEdge.source,
            target: outEdge.target,
            ...(inEdge.sourceHandle ? { sourceHandle: inEdge.sourceHandle } : {}),
          });
        }
      }

      if (draft.selectedNodeId === nodeId) {
        draft.selectedNodeId = definition.trigger?.id ?? null;
        draft.isNodeConfigModalOpen = false;
      }
    })
  );
});

// Seeds the trigger on an empty draft (the "Add trigger" picker). The placeholder surveyId, a random UUID that names no survey, keeps the config storable until the user binds a real survey; the config panel opens at once to walk them into it.
export const addWorkflowTriggerAtom = atom(null, (get, set, triggerType: TWorkflowTriggerType) => {
  set(
    workflowEditorAtom,
    produce(get(workflowEditorAtom), (draft) => {
      const definition = draft.definition;
      if (!definition || definition.trigger) return;
      if (triggerType !== WORKFLOW_TRIGGERS.RESPONSE_COMPLETED) return;

      const triggerId = createId();
      definition.trigger = {
        id: triggerId,
        type: "trigger",
        triggerType,
        config: { surveyId: crypto.randomUUID(), endingCardIds: [] },
        // WORKFLOW_CANVAS_START_POSITION of definition-to-flow, not imported to avoid a cycle.
        ui: { position: { x: 220, y: 80 } },
      };
      definition.entryNodeId = triggerId;

      draft.selectedNodeId = triggerId;
      draft.isNodeConfigModalOpen = true;
    })
  );
});

// Where a new node goes when the node or edge it is placed against has no stored position.
const SEND_EMAIL_FALLBACK_POSITION = { x: 220, y: 200 };

// The send_email node both insertions create. No label, so the title falls back to the translated default; the position is cloned so no two nodes share one object.
const createDefaultSendEmailNode = (position: {
  x: number;
  y: number;
}): TWorkflowDefinition["nodes"][number] => ({
  id: createId(),
  type: "action",
  actionType: WORKFLOW_ACTIONS.SEND_EMAIL,
  config: {
    to: "",
    from: "team@example.com",
    replyTo: [],
    subject: "",
    body: "",
    attachResponseData: false,
  },
  ui: { position: { ...position } },
});

// Appends a send_email node after sourceNodeId, 120px below it.
export const appendSendEmailAfterNodeAtom = atom(null, (get, set, sourceNodeId: string) => {
  set(
    workflowEditorAtom,
    produce(get(workflowEditorAtom), (draft) => {
      const definition = draft.definition;
      if (!definition) return;

      const sourcePosition =
        definition.trigger?.id === sourceNodeId
          ? definition.trigger.ui?.position
          : definition.nodes.find((node) => node.id === sourceNodeId)?.ui?.position;
      const newPosition = sourcePosition
        ? { x: sourcePosition.x, y: sourcePosition.y + 120 }
        : SEND_EMAIL_FALLBACK_POSITION;

      const newNode = createDefaultSendEmailNode(newPosition);
      definition.nodes.push(newNode);
      definition.edges.push({ id: createId(), source: sourceNodeId, target: newNode.id });

      draft.selectedNodeId = newNode.id;
      draft.isNodeConfigModalOpen = true;
    })
  );
});

// Splits an edge with a send_email node at its midpoint, keeping the edge's sourceHandle on the first half.
export const insertSendEmailAfterEdgeAtom = atom(null, (get, set, edgeId: string) => {
  set(
    workflowEditorAtom,
    produce(get(workflowEditorAtom), (draft) => {
      const definition = draft.definition;
      if (!definition) return;
      const edgeIndex = definition.edges.findIndex((edge) => edge.id === edgeId);
      if (edgeIndex < 0) return;
      const edge = definition.edges[edgeIndex];
      if (!edge) return;

      const positionOf = (nodeId: string) => {
        if (definition.trigger?.id === nodeId) return definition.trigger.ui?.position;
        return definition.nodes.find((node) => node.id === nodeId)?.ui?.position;
      };
      const sourcePosition = positionOf(edge.source);
      const targetPosition = positionOf(edge.target);
      const midpoint =
        sourcePosition && targetPosition
          ? {
              x: Math.round((sourcePosition.x + targetPosition.x) / 2),
              y: Math.round((sourcePosition.y + targetPosition.y) / 2),
            }
          : SEND_EMAIL_FALLBACK_POSITION;

      const newNode = createDefaultSendEmailNode(midpoint);
      definition.nodes.push(newNode);
      definition.edges.splice(
        edgeIndex,
        1,
        {
          id: createId(),
          source: edge.source,
          target: newNode.id,
          ...(edge.sourceHandle ? { sourceHandle: edge.sourceHandle } : {}),
        },
        { id: createId(), source: newNode.id, target: edge.target }
      );

      draft.selectedNodeId = newNode.id;
      draft.isNodeConfigModalOpen = true;
    })
  );
});

export const setWorkflowSnapToCanvasEnabledAtom = atom(null, (get, set, isSnapToCanvasEnabled: boolean) => {
  set(
    workflowEditorAtom,
    produce(get(workflowEditorAtom), (draft) => {
      draft.isSnapToCanvasEnabled = isSnapToCanvasEnabled;
    })
  );
});
