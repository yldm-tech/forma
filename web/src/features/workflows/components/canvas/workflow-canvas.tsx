import {
  Background,
  BackgroundVariant,
  type EdgeTypes,
  type Node,
  type NodeTypes,
  type OnNodesChange,
  ReactFlow,
  ReactFlowProvider,
  applyNodeChanges,
  useReactFlow,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { useAtomValue, useSetAtom } from "jotai";
import { PanelLeftIcon, PanelRightOpenIcon } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../../components/ui/button";
import { cn } from "../../../../lib/cn";
import {
  WORKFLOW_CANVAS_NODE_TYPE,
  WORKFLOW_CANVAS_SNAP_GRID,
  reorganizeWorkflowDefinition,
  snapWorkflowNodePosition,
  updateNodePosition,
  workflowDefinitionToFlowEdges,
  workflowDefinitionToFlowNodes,
} from "../../lib/definition-to-flow";
import {
  type TWorkflowNodeData,
  addWorkflowTriggerAtom,
  closeWorkflowNodeConfigModalAtom,
  hasBoundTriggerSurveyAtom,
  isCanvasLockedAtom,
  isWorkflowInspectorCollapsedAtom,
  isWorkflowNodeConfigModalOpenAtom,
  isWorkflowSnapToCanvasEnabledAtom,
  openWorkflowNodeConfigModalAtom,
  setWorkflowDefinitionAtom,
  toggleWorkflowInspectorAtom,
  workflowAtom,
  workflowDefinitionAtom,
  workflowFlowNodesAtom,
} from "../../state/editor";
import { AddButtonEdge } from "./add-button-edge";
import { CanvasControls } from "./canvas-controls";
import { WorkflowAddTriggerPicker } from "./workflow-add-trigger-picker";
import { WorkflowCanvasNode } from "./workflow-canvas-node";
import "./workflow-canvas.css";
import { WorkflowValidationStatus } from "./workflow-validation-status";

const NODE_TYPES: NodeTypes = { [WORKFLOW_CANVAS_NODE_TYPE]: WorkflowCanvasNode };

const EDGE_TYPES: EdgeTypes = { addButton: AddButtonEdge };

// fitView may scale a small flow up to this, three zoom steps (1.2x each) below 2x.
const WORKFLOW_CANVAS_MAX_ZOOM = 1.15;

// The inspector column animates its width over 150 ms; the viewport is refit once the canvas has its final size.
const INSPECTOR_RESIZE_SETTLE_MS = 170;
const INSPECTOR_REFIT_PAN_MS = INSPECTOR_RESIZE_SETTLE_MS / 4;

const prefersReducedMotion = () => window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false;

const WorkflowCanvasContent = ({ isEditable }: Readonly<{ isEditable: boolean }>) => {
  const { t } = useTranslation();
  const workflow = useAtomValue(workflowAtom);
  const definition = useAtomValue(workflowDefinitionAtom);
  const flowNodes = useAtomValue(workflowFlowNodesAtom);
  const isSnapToCanvasEnabled = useAtomValue(isWorkflowSnapToCanvasEnabledAtom);
  const isLocked = useAtomValue(isCanvasLockedAtom);
  const setLocked = useSetAtom(isCanvasLockedAtom);
  const isInspectorCollapsed = useAtomValue(isWorkflowInspectorCollapsedAtom);
  const isNodeConfigOpen = useAtomValue(isWorkflowNodeConfigModalOpenAtom);
  const toggleInspector = useSetAtom(toggleWorkflowInspectorAtom);
  const closeNodeConfig = useSetAtom(closeWorkflowNodeConfigModalAtom);
  const setDefinition = useSetAtom(setWorkflowDefinitionAtom);
  const setFlowNodes = useSetAtom(workflowFlowNodesAtom);
  const openNodeConfigModal = useSetAtom(openWorkflowNodeConfigModalAtom);
  const addTrigger = useSetAtom(addWorkflowTriggerAtom);
  const { fitView } = useReactFlow();
  // isEditable is the API's gate (status and permissions); the pan/pointer toggle is the user's gate on top of it.
  const canMutate = isEditable && !isLocked;
  // While the centred add-trigger picker is the canvas's main event, the validation chip would only restate it.
  const isTriggerPickerVisible = Boolean(definition && !definition.trigger && isEditable);

  const hasBoundSurvey = useAtomValue(hasBoundTriggerSurveyAtom);
  // Unloaded defaults to draft, so a fresh page never flashes red before the workflow lands.
  const isDraft = workflow ? workflow.status === "draft" : true;

  const derivedFlowNodes = useMemo(
    () => (definition ? workflowDefinitionToFlowNodes(definition, t, { hasBoundSurvey, isDraft }) : []),
    [definition, t, hasBoundSurvey, isDraft]
  );
  const flowEdges = useMemo(
    () => (definition ? workflowDefinitionToFlowEdges(definition) : []),
    [definition]
  );

  // React Flow's nodes follow the definition while keeping the user's selection, which recomputing from scratch would lose on every edit.
  useEffect(() => {
    setFlowNodes((currentNodes) => {
      const currentNodesById = new Map(currentNodes.map((node) => [node.id, node]));
      return derivedFlowNodes.map((node) => ({
        ...node,
        selected: currentNodesById.get(node.id)?.selected ?? node.selected,
      }));
    });
  }, [derivedFlowNodes, setFlowNodes]);

  const handleNodesChange: OnNodesChange<Node<TWorkflowNodeData>> = useCallback(
    (changes) => setFlowNodes((currentNodes) => applyNodeChanges(changes, currentNodes)),
    [setFlowNodes]
  );

  const handleNodeDragStop = useCallback(
    (node: Node<TWorkflowNodeData>) => {
      if (!canMutate) return;
      const position = isSnapToCanvasEnabled ? snapWorkflowNodePosition(node.position) : node.position;
      setDefinition((current) => (current ? updateNodePosition(current, node.id, position) : current));
    },
    [canMutate, isSnapToCanvasEnabled, setDefinition]
  );

  // Opening or closing the inspector resizes the canvas, which can push nodes behind the panel's edge; the viewport (never the nodes) is refit once the width settles.
  const isInspectorVisible = isNodeConfigOpen && !isInspectorCollapsed;
  const previousInspectorVisibleRef = useRef(isInspectorVisible);
  useEffect(() => {
    if (previousInspectorVisibleRef.current === isInspectorVisible) return;
    previousInspectorVisibleRef.current = isInspectorVisible;
    const timeoutHandle = setTimeout(() => {
      void fitView({
        padding: 0.25,
        maxZoom: WORKFLOW_CANVAS_MAX_ZOOM,
        minZoom: 0.4,
        duration: prefersReducedMotion() ? 0 : INSPECTOR_REFIT_PAN_MS,
      });
    }, INSPECTOR_RESIZE_SETTLE_MS / 2);
    return () => clearTimeout(timeoutHandle);
  }, [isInspectorVisible, fitView]);

  const handleAutoLayout = useCallback(() => {
    if (!canMutate) return;
    setDefinition((current) => (current ? reorganizeWorkflowDefinition(current) : current));
    // One frame later, so the new positions render before the viewport recentres.
    requestAnimationFrame(() => {
      void fitView({
        padding: 0.25,
        maxZoom: WORKFLOW_CANVAS_MAX_ZOOM,
        minZoom: 0.4,
        duration: prefersReducedMotion() ? 0 : 300,
      });
    });
  }, [canMutate, setDefinition, fitView]);

  // Pan mode makes nodes inert, so switching into it clears any selection left behind.
  const handlePanMode = () => {
    setLocked(true);
    setFlowNodes((currentNodes) =>
      currentNodes.map((node) => (node.selected ? { ...node, selected: false } : node))
    );
  };

  return (
    <div className="relative min-h-0 min-w-0 flex-1 overflow-hidden rounded-lg border border-slate-200 bg-white">
      {isNodeConfigOpen ? (
        <div className="absolute top-4 right-4 z-10">
          <Button
            variant="outline"
            size="icon"
            className="bg-white"
            aria-label={
              isInspectorCollapsed
                ? t("workspace.workflows.expand_inspector")
                : t("workspace.workflows.collapse_inspector")
            }
            aria-pressed={!isInspectorCollapsed}
            onClick={toggleInspector}>
            {isInspectorCollapsed ? <PanelRightOpenIcon /> : <PanelLeftIcon />}
          </Button>
        </div>
      ) : null}
      <ReactFlow
        nodes={flowNodes}
        edges={flowEdges}
        nodeTypes={NODE_TYPES}
        edgeTypes={EDGE_TYPES}
        onNodesChange={handleNodesChange}
        onNodeDragStop={(_event, node) => handleNodeDragStop(node)}
        onNodeClick={(_event, node) => {
          if (!isLocked) openNodeConfigModal(node.id);
        }}
        // A click on the empty canvas deselects and dismisses the inspector.
        onPaneClick={() => closeNodeConfig()}
        className={cn("workflow-canvas bg-slate-50", isLocked && "pan-mode")}
        fitView
        fitViewOptions={{ padding: 0.25, maxZoom: WORKFLOW_CANVAS_MAX_ZOOM, minZoom: 0.4 }}
        defaultViewport={{ x: 0, y: 0, zoom: WORKFLOW_CANVAS_MAX_ZOOM }}
        nodesDraggable={canMutate}
        // Without a definition edit behind it, Backspace would drop a node from the canvas only, which a reload brings back; it is off while the canvas cannot be changed.
        deleteKeyCode={canMutate ? "Backspace" : null}
        nodesConnectable={false}
        snapGrid={WORKFLOW_CANVAS_SNAP_GRID}
        snapToGrid={isSnapToCanvasEnabled}
        proOptions={{ hideAttribution: true }}
        elementsSelectable={!isLocked}
        nodesFocusable={!isLocked}>
        <Background variant={BackgroundVariant.Dots} gap={18} size={1.5} color="#cbd5e1" />
      </ReactFlow>
      {/* A new draft has no nodes: the centred picker is how the trigger gets added. */}
      {isTriggerPickerVisible && (
        <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center">
          <div className="pointer-events-auto">
            <WorkflowAddTriggerPicker onSelect={addTrigger} />
          </div>
        </div>
      )}
      <CanvasControls
        canMutate={canMutate}
        isPanMode={isLocked}
        onAutoLayout={handleAutoLayout}
        onPanMode={handlePanMode}
        onPointerMode={() => setLocked(false)}
      />
      {!isTriggerPickerVisible && <WorkflowValidationStatus />}
    </div>
  );
};

/** forma2's WorkflowCanvas: the definition drawn with React Flow, with the add-trigger picker on an empty draft, the canvas controls and the live validation state. */
export const WorkflowCanvas = (props: Readonly<{ isEditable: boolean }>) => (
  <ReactFlowProvider>
    <WorkflowCanvasContent {...props} />
  </ReactFlowProvider>
);
