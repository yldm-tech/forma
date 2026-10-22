import type { TFunction } from "i18next";
import { describe, expect, test } from "vite-plus/test";
import type { TWorkflowDefinition } from "../definition";
import {
  reorganizeWorkflowDefinition,
  snapWorkflowNodePosition,
  updateNodePosition,
  workflowDefinitionToFlowEdges,
  workflowDefinitionToFlowNodes,
} from "./definition-to-flow";

const t = ((key: string) => key) as unknown as TFunction;

const buildDefinition = (overrides?: Partial<TWorkflowDefinition>): TWorkflowDefinition =>
  ({
    schemaVersion: 1,
    entryNodeId: "trigger-1",
    trigger: {
      id: "trigger-1",
      type: "trigger",
      triggerType: "response.completed",
      config: { surveyId: "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0e", endingCardIds: [] },
      ui: { position: { x: 0, y: 0 } },
    },
    nodes: [
      {
        id: "action-1",
        type: "action",
        actionType: "send_email",
        label: "Send email",
        config: {
          to: "respondent@example.com",
          from: "team@example.com",
          replyTo: [],
          subject: "Thanks",
          body: "Body",
          attachResponseData: false,
        },
        ui: { position: { x: 0, y: 0 } },
      },
    ],
    edges: [{ id: "edge-1", source: "trigger-1", target: "action-1" }],
    ...overrides,
  }) as TWorkflowDefinition;

describe("snapWorkflowNodePosition", () => {
  test("snaps to the 20px grid", () => {
    expect(snapWorkflowNodePosition({ x: 7, y: 31 })).toEqual({ x: 0, y: 40 });
    expect(snapWorkflowNodePosition({ x: 31, y: 49 })).toEqual({ x: 40, y: 40 });
  });
});

describe("updateNodePosition", () => {
  test("updates trigger position when id matches", () => {
    const def = buildDefinition();
    const next = updateNodePosition(def, "trigger-1", { x: 100, y: 80 });
    expect(next.trigger?.ui?.position).toEqual({ x: 100, y: 80 });
    expect(next.nodes[0]?.ui?.position).toEqual({ x: 0, y: 0 });
  });

  test("updates child-node position when id matches", () => {
    const def = buildDefinition();
    const next = updateNodePosition(def, "action-1", { x: 220, y: 240 });
    expect(next.trigger?.ui?.position).toEqual({ x: 0, y: 0 });
    expect(next.nodes[0]?.ui?.position).toEqual({ x: 220, y: 240 });
  });
});

describe("workflowDefinitionToFlowNodes / Edges", () => {
  test("projects trigger and child nodes with registry metadata", () => {
    const def = buildDefinition();
    const nodes = workflowDefinitionToFlowNodes(def, t);
    expect(nodes).toHaveLength(2);
    expect(nodes[0]).toMatchObject({
      id: "trigger-1",
      data: { category: "trigger", icon: "trigger", title: "workspace.workflows.response_completed" },
    });
    expect(nodes[1]).toMatchObject({
      id: "action-1",
      data: { category: "action", icon: "email" },
    });
  });

  test("flags no issues by default (survey resolution unknown)", () => {
    const nodes = workflowDefinitionToFlowNodes(buildDefinition(), t);
    expect(nodes.map((node) => node.data.issue)).toEqual([null, null]);
  });

  test("flags only the trigger when the bound survey doesn't resolve (sequential guidance)", () => {
    const nodes = workflowDefinitionToFlowNodes(buildDefinition(), t, {
      hasBoundSurvey: false,
      isDraft: true,
    });
    expect(nodes[0]?.data.issue).toEqual({
      severity: "setup",
      label: "workspace.workflows.node_needs_survey",
    });
    expect(nodes[1]?.data.issue).toBeNull();
  });

  test("flags an incomplete email node once the survey is bound, with error severity when live", () => {
    const def = buildDefinition();
    (def.nodes[0] as { config: { to: string } }).config.to = "";
    const nodes = workflowDefinitionToFlowNodes(def, t, { hasBoundSurvey: true, isDraft: false });
    expect(nodes[0]?.data.issue).toBeNull();
    expect(nodes[1]?.data.issue).toEqual({
      severity: "error",
      label: "workspace.workflows.node_needs_email_content",
    });
  });

  test("flags an email node whose subject is blank (parity with the executable schema)", () => {
    const def = buildDefinition();
    (def.nodes[0] as { config: { subject: string } }).config.subject = "   ";
    const nodes = workflowDefinitionToFlowNodes(def, t, { hasBoundSurvey: true, isDraft: true });
    expect(nodes[1]?.data.issue).toEqual({
      severity: "setup",
      label: "workspace.workflows.node_needs_email_content",
    });
  });

  test("flags an email node whose body is only the editor's empty markup", () => {
    // The rich-text editor keeps the enclosing block when the body is emptied, so a trim-based
    // check would clear the flag on a step that has nothing to send.
    const def = buildDefinition();
    (def.nodes[0] as { config: { body: string } }).config.body = '<p class="fb-editor-paragraph"><br></p>';
    const nodes = workflowDefinitionToFlowNodes(def, t, { hasBoundSurvey: true, isDraft: true });
    expect(nodes[1]?.data.issue).toEqual({
      severity: "setup",
      label: "workspace.workflows.node_needs_email_content",
    });
  });

  test("projects edges with sourceHandle preserved", () => {
    const def = buildDefinition({
      edges: [{ id: "edge-1", source: "trigger-1", target: "action-1", sourceHandle: "then" }],
    });
    const edges = workflowDefinitionToFlowEdges(def);
    expect(edges[0]).toMatchObject({
      id: "edge-1",
      source: "trigger-1",
      target: "action-1",
      sourceHandle: "then",
      type: "addButton",
    });
  });
});

describe("reorganizeWorkflowDefinition", () => {
  test("ranks reachable nodes by BFS distance from the trigger", () => {
    const def = buildDefinition();
    const next = reorganizeWorkflowDefinition(def);
    const triggerY = next.trigger?.ui?.position?.y ?? 0;
    const actionY = next.nodes[0]?.ui?.position?.y ?? 0;
    expect(actionY).toBeGreaterThan(triggerY);
  });

  test("places unreachable nodes below the reachable graph", () => {
    const base = buildDefinition();
    const orphan = { ...base.nodes[0], id: "orphan", ui: undefined } as (typeof base.nodes)[number];
    const def: TWorkflowDefinition = { ...base, nodes: [...base.nodes, orphan] };

    const next = reorganizeWorkflowDefinition(def);
    const reachableY = next.nodes[0]?.ui?.position?.y ?? 0;
    const orphanY = next.nodes[1]?.ui?.position?.y ?? 0;
    expect(orphanY).toBeGreaterThan(reachableY);
  });
});

describe("workflowDefinitionToFlowNodes fallback", () => {
  test("falls back to a derived position when ui.position is missing", () => {
    const base = buildDefinition();
    const triggerWithoutUi = { ...base.trigger, ui: undefined };
    const def = { ...base, trigger: triggerWithoutUi } as TWorkflowDefinition;
    const nodes = workflowDefinitionToFlowNodes(def, t);
    expect(nodes[0]?.position).toEqual({ x: 120, y: 80 });
  });
});

describe("trigger-less draft definitions", () => {
  const emptyDefinition = buildDefinition({
    trigger: null,
    nodes: [],
    edges: [],
    entryNodeId: null,
  });

  test("project to zero flow nodes and edges", () => {
    expect(workflowDefinitionToFlowNodes(emptyDefinition, t)).toEqual([]);
    expect(workflowDefinitionToFlowEdges(emptyDefinition)).toEqual([]);
  });

  test("updateNodePosition and reorganize are no-op safe", () => {
    expect(updateNodePosition(emptyDefinition, "anything", { x: 1, y: 1 }).trigger).toBeNull();
    expect(reorganizeWorkflowDefinition(emptyDefinition).trigger).toBeNull();
  });
});

describe("canvas layout constants", () => {
  test("every node is a workflowCanvasNode, an edge is an addButton labelled with its handle, and a leaf is a node without an outgoing edge", () => {
    const def = buildDefinition();
    const nodes = workflowDefinitionToFlowNodes(def, t);
    expect(nodes.map((node) => node.type)).toEqual(["workflowCanvasNode", "workflowCanvasNode"]);
    expect(nodes.map((node) => node.data.isLeaf)).toEqual([false, true]);
    expect(nodes.map((node) => node.data.nodeType)).toEqual(["response.completed", "send_email"]);
    const [edge] = workflowDefinitionToFlowEdges(
      buildDefinition({
        edges: [{ id: "edge-1", source: "trigger-1", target: "action-1", sourceHandle: "else" }],
      })
    );
    expect(edge).toMatchObject({ type: "addButton", label: "else" });
  });

  test("auto-layout starts at (220, 80) and steps 120px per rank, rows centred 360px apart", () => {
    const base = buildDefinition();
    const second = { ...base.nodes[0], id: "action-2" } as (typeof base.nodes)[number];
    const def: TWorkflowDefinition = {
      ...base,
      nodes: [...base.nodes, second],
      edges: [...base.edges, { id: "edge-2", source: "trigger-1", target: "action-2" }],
    };
    const next = reorganizeWorkflowDefinition(def);
    expect(next.trigger?.ui?.position).toEqual({ x: 220, y: 80 });
    expect(next.nodes.map((node) => node.ui?.position)).toEqual([
      { x: 40, y: 200 },
      { x: 400, y: 200 },
    ]);
  });

  test("a node without a stored position falls back to a column 120px apart", () => {
    const base = buildDefinition();
    const def = {
      ...base,
      trigger: { ...base.trigger, ui: undefined },
      nodes: base.nodes.map((node) => ({ ...node, ui: undefined })),
    } as TWorkflowDefinition;
    expect(workflowDefinitionToFlowNodes(def, t).map((node) => node.position)).toEqual([
      { x: 120, y: 80 },
      { x: 120, y: 200 },
    ]);
  });

  test("a send_email node shows its label as title and its recipient as summary", () => {
    const [, action] = workflowDefinitionToFlowNodes(buildDefinition(), t);
    expect(action?.data).toMatchObject({
      title: "Send email",
      summary: "workspace.workflows.send_email_summary",
    });
  });
});
