import type { TWorkflowDefinition } from "../definition";

/** A new workflow starts with an empty canvas: the user adds the trigger and actions from the canvas pickers. */
export const createEmptyWorkflowDefinition = (): TWorkflowDefinition => ({
  schemaVersion: 1,
  trigger: null,
  nodes: [],
  edges: [],
  entryNodeId: null,
});
