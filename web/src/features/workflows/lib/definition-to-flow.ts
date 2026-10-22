import type { Edge, Node, SnapGrid } from "@xyflow/react";
import type { TFunction } from "i18next";
import {
  type TWorkflowDefinition,
  type TWorkflowNode,
  getBlankSendEmailContentFields,
  getWorkflowNodeConcreteType,
} from "../definition";
import type { TWorkflowNodeData, TWorkflowNodeIssue } from "../state/editor";
import { getNodeRegistryEntry } from "./node-registry";

// The definition as React Flow nodes and edges, and the layout helpers the canvas uses, forma2's modules/workflows/lib/definition-to-flow.ts.

export const WORKFLOW_CANVAS_NODE_TYPE = "workflowCanvasNode";

export const WORKFLOW_CANVAS_SNAP_GRID: SnapGrid = [20, 20];

const WORKFLOW_CANVAS_NODE_SPACING = { x: 360, y: 120 };
const WORKFLOW_CANVAS_START_POSITION = { x: 220, y: 80 };

interface WorkflowFlowNodeOptions {
  /** Whether the trigger's surveyId resolves to a survey. Callers that cannot know (the first hydrate) default to true and are corrected on the next render. */
  hasBoundSurvey?: boolean;
  /** Drafts get "setup" severity, since an unfinished config is a draft's normal state; live or previously live workflows get "error". */
  isDraft?: boolean;
}

// The client's counterpart of the dry run's checks: a node that cannot run as configured. While the survey is missing only the trigger is flagged, since the email step cannot be configured before there is a survey.
const getWorkflowNodeIssue = (
  node: TWorkflowNode,
  t: TFunction,
  options: Required<WorkflowFlowNodeOptions>
): TWorkflowNodeIssue | null => {
  const severity = options.isDraft ? "setup" : "error";
  if (node.type === "trigger" && !options.hasBoundSurvey) {
    return { severity, label: t("workspace.workflows.node_needs_survey") };
  }
  if (node.type === "action" && node.actionType === "send_email" && options.hasBoundSurvey) {
    if (getBlankSendEmailContentFields(node.config).length > 0) {
      return { severity, label: t("workspace.workflows.node_needs_email_content") };
    }
  }
  return null;
};

export const workflowDefinitionToFlowNodes = (
  definition: TWorkflowDefinition,
  t: TFunction,
  options?: WorkflowFlowNodeOptions
): Array<Node<TWorkflowNodeData>> => {
  const sourcesWithEdges = new Set(definition.edges.map((edge) => edge.source));
  const resolvedOptions: Required<WorkflowFlowNodeOptions> = {
    hasBoundSurvey: options?.hasBoundSurvey ?? true,
    isDraft: options?.isDraft ?? true,
  };
  const nodes: TWorkflowNode[] = [...(definition.trigger ? [definition.trigger] : []), ...definition.nodes];
  return nodes.map((node, index) => {
    const registryEntry = getNodeRegistryEntry(node);
    return {
      id: node.id,
      type: WORKFLOW_CANVAS_NODE_TYPE,
      position: node.ui?.position ?? { x: 120, y: 80 + index * 120 },
      data: {
        category: registryEntry.category,
        icon: registryEntry.icon,
        nodeType: getWorkflowNodeConcreteType(node),
        title: registryEntry.title(node, t),
        summary: registryEntry.summary(node, t),
        isLeaf: !sourcesWithEdges.has(node.id),
        issue: getWorkflowNodeIssue(node, t, resolvedOptions),
      },
    };
  });
};

export const workflowDefinitionToFlowEdges = (definition: TWorkflowDefinition): Edge[] =>
  definition.edges.map((edge) => ({
    id: edge.id,
    source: edge.source,
    target: edge.target,
    sourceHandle: edge.sourceHandle,
    targetHandle: edge.targetHandle,
    label: edge.sourceHandle,
    type: "addButton",
  }));

export const snapWorkflowNodePosition = (position: { x: number; y: number }) => ({
  x: Math.round(position.x / WORKFLOW_CANVAS_SNAP_GRID[0]) * WORKFLOW_CANVAS_SNAP_GRID[0],
  y: Math.round(position.y / WORKFLOW_CANVAS_SNAP_GRID[1]) * WORKFLOW_CANVAS_SNAP_GRID[1],
});

export const updateNodePosition = (
  definition: TWorkflowDefinition,
  nodeId: string,
  position: { x: number; y: number }
): TWorkflowDefinition => {
  if (definition.trigger?.id === nodeId) {
    return { ...definition, trigger: { ...definition.trigger, ui: { ...definition.trigger.ui, position } } };
  }
  return {
    ...definition,
    nodes: definition.nodes.map((node) =>
      node.id === nodeId ? { ...node, ui: { ...node.ui, position } } : node
    ),
  };
};

// Breadth-first rank from the trigger, so reachable nodes at the same depth share a row; unreachable nodes, and every node of a trigger-less draft, each get a row of their own below, in their original order.
const rankWorkflowNodes = (
  definition: TWorkflowDefinition,
  nodesById: Map<string, TWorkflowNode>,
  originalNodeOrder: string[]
): Map<string, number> => {
  const edgesBySource = new Map<string, string[]>();
  for (const edge of definition.edges) {
    edgesBySource.set(edge.source, [...(edgesBySource.get(edge.source) ?? []), edge.target]);
  }

  const ranks = new Map<string, number>(definition.trigger ? [[definition.trigger.id, 0]] : []);
  const queue = definition.trigger ? [definition.trigger.id] : [];
  while (queue.length > 0) {
    const nodeId = queue.shift() as string;
    const nextRank = (ranks.get(nodeId) ?? 0) + 1;
    for (const targetId of edgesBySource.get(nodeId) ?? []) {
      if (!nodesById.has(targetId) || ranks.has(targetId)) continue;
      ranks.set(targetId, nextRank);
      queue.push(targetId);
    }
  }

  let nextUnreachableRank = Math.max(0, ...ranks.values()) + 1;
  for (const nodeId of originalNodeOrder) {
    if (!ranks.has(nodeId)) {
      ranks.set(nodeId, nextUnreachableRank);
      nextUnreachableRank += 1;
    }
  }
  return ranks;
};

/** The auto-layout: every node placed on a grid by its rank from the trigger, rows centred, positions snapped. */
export const reorganizeWorkflowDefinition = (definition: TWorkflowDefinition): TWorkflowDefinition => {
  const allNodes: TWorkflowNode[] = [
    ...(definition.trigger ? [definition.trigger] : []),
    ...definition.nodes,
  ];
  const nodesById = new Map(allNodes.map((node) => [node.id, node]));
  const originalNodeOrder = allNodes.map((node) => node.id);
  const ranks = rankWorkflowNodes(definition, nodesById, originalNodeOrder);

  const nodeIdsByRank = new Map<number, string[]>();
  for (const nodeId of originalNodeOrder) {
    const rank = ranks.get(nodeId) ?? 0;
    nodeIdsByRank.set(rank, [...(nodeIdsByRank.get(rank) ?? []), nodeId]);
  }

  const positionsByNodeId = new Map<string, { x: number; y: number }>();
  for (const [rank, nodeIds] of nodeIdsByRank) {
    const rowOffset = ((nodeIds.length - 1) * WORKFLOW_CANVAS_NODE_SPACING.x) / 2;
    nodeIds.forEach((nodeId, index) => {
      positionsByNodeId.set(
        nodeId,
        snapWorkflowNodePosition({
          x: WORKFLOW_CANVAS_START_POSITION.x + index * WORKFLOW_CANVAS_NODE_SPACING.x - rowOffset,
          y: WORKFLOW_CANVAS_START_POSITION.y + rank * WORKFLOW_CANVAS_NODE_SPACING.y,
        })
      );
    });
  }

  return {
    ...definition,
    trigger: definition.trigger
      ? {
          ...definition.trigger,
          ui: {
            ...definition.trigger.ui,
            position: positionsByNodeId.get(definition.trigger.id) ?? definition.trigger.ui?.position,
          },
        }
      : null,
    nodes: definition.nodes.map((node) => ({
      ...node,
      ui: { ...node.ui, position: positionsByNodeId.get(node.id) ?? node.ui?.position },
    })),
  };
};
