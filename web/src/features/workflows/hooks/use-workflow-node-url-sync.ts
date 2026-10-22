import { useAtomValue, useSetAtom } from "jotai";
import { useEffect, useState } from "react";
import {
  isWorkflowNodeConfigModalOpenAtom,
  openWorkflowNodeConfigModalAtom,
  selectedWorkflowNodeIdAtom,
  workflowDefinitionAtom,
  workflowFlowNodesAtom,
} from "../state/editor";

interface UseWorkflowNodeUrlSyncArgs {
  /** Nothing happens until the caller has hydrated the editor. */
  isEnabled: boolean;
  /** The current ?node= value. */
  nodeParam: string | null;
  /** Writes ?node= (null removes it), replacing the history entry: browsing nodes is not a history the Back button should walk. */
  setNodeParam: (nodeId: string | null) => void;
}

/** forma2's useWorkflowNodeUrlSync: the inspected node mirrored into ?node=, so a node's config is deep-linkable and survives a reload. On the first hydrate a valid ?node= opens that node, as a click would; an unknown id is ignored and cleaned up. */
export const useWorkflowNodeUrlSync = ({
  isEnabled,
  nodeParam,
  setNodeParam,
}: UseWorkflowNodeUrlSyncArgs) => {
  const definition = useAtomValue(workflowDefinitionAtom);
  const selectedNodeId = useAtomValue(selectedWorkflowNodeIdAtom);
  const isNodeConfigOpen = useAtomValue(isWorkflowNodeConfigModalOpenAtom);
  const openNodeConfigModal = useSetAtom(openWorkflowNodeConfigModalAtom);
  const setFlowNodes = useSetAtom(workflowFlowNodesAtom);

  // State rather than a ref, so the write-back effect stays inert in the render that applies the initial param and does not wipe it before the panel's open state lands.
  const [hasAppliedInitialNode, setHasAppliedInitialNode] = useState(false);

  useEffect(() => {
    if (!isEnabled || hasAppliedInitialNode || !definition) return;
    setHasAppliedInitialNode(true);
    if (!nodeParam) return;
    const nodeExists =
      definition.trigger?.id === nodeParam || definition.nodes.some((node) => node.id === nodeParam);
    if (!nodeExists) return;
    openNodeConfigModal(nodeParam);
    // What a canvas click does: React Flow marks the clicked node selected.
    setFlowNodes((currentNodes) =>
      currentNodes.map((node) =>
        node.selected === (node.id === nodeParam) ? node : { ...node, selected: node.id === nodeParam }
      )
    );
  }, [isEnabled, hasAppliedInitialNode, definition, nodeParam, openNodeConfigModal, setFlowNodes]);

  useEffect(() => {
    if (!isEnabled || !hasAppliedInitialNode) return;
    const nextParam = isNodeConfigOpen && selectedNodeId ? selectedNodeId : null;
    if ((nodeParam ?? null) === nextParam) return;
    setNodeParam(nextParam);
  }, [isEnabled, hasAppliedInitialNode, isNodeConfigOpen, selectedNodeId, nodeParam, setNodeParam]);
};
