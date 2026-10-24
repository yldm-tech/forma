import { useAtomValue, useSetAtom } from "jotai";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Alert, AlertDescription } from "../../../../components/ui/alert";
import {
  type TWorkflowDefinition,
  type TWorkflowNode,
  type TWorkflowStatus,
  getWorkflowNodeConcreteType,
} from "../../definition";
import { WORKFLOW_CLIENT_EVENTS, trackWorkflowEvent } from "../../lib/analytics";
import { getNodeRegistryEntry } from "../../lib/node-registry";
import {
  isWorkflowReadOnlyAtom,
  selectedWorkflowNodeIdAtom,
  setWorkflowDefinitionAtom,
  workflowDefinitionAtom,
  workflowStatusAtom,
} from "../../state/editor";

const findSelectedNode = (
  definition: TWorkflowDefinition | null,
  selectedNodeId: string | null
): TWorkflowNode | null => {
  if (!definition || !selectedNodeId) return null;
  if (definition.trigger?.id === selectedNodeId) return definition.trigger;
  return definition.nodes.find((node) => node.id === selectedNodeId) ?? null;
};

// Why the config cannot be changed: a missing permission wins over an archived workflow, which wins over an enabled one.
type TBlockedReason = "readOnly" | "archived" | "active";

const getBlockedReason = (
  isEditable: boolean,
  isReadOnly: boolean,
  status: TWorkflowStatus | null
): TBlockedReason | null => {
  if (isEditable) return null;
  if (isReadOnly) return "readOnly";
  if (status === "archived") return "archived";
  return "active";
};

const replaceNode = (definition: TWorkflowDefinition, node: TWorkflowNode): TWorkflowDefinition => {
  if (node.type === "trigger") {
    return node.id === definition.trigger?.id ? { ...definition, trigger: node } : definition;
  }
  return {
    ...definition,
    nodes: definition.nodes.map((existing) => (existing.id === node.id ? node : existing)),
  };
};

/** forma2's WorkflowNodeConfigPanel: the selected node's form. Every change writes straight into the definition, so the canvas card and the validity update live and the autosave persists it. */
export const WorkflowNodeConfigPanel = ({ isEditable }: Readonly<{ isEditable: boolean }>) => {
  const { t } = useTranslation();
  const definition = useAtomValue(workflowDefinitionAtom);
  const selectedNodeId = useAtomValue(selectedWorkflowNodeIdAtom);
  // The status alone, not the workflow: every save replaces the workflow, which would re-render the email editor this panel hosts.
  const status = useAtomValue(workflowStatusAtom);
  const isReadOnly = useAtomValue(isWorkflowReadOnlyAtom);
  const setDefinition = useSetAtom(setWorkflowDefinitionAtom);

  const selectedNode = findSelectedNode(definition, selectedNodeId);

  // Which node kinds people open, and whether they land on one with no form yet (if_else). Keyed on the node, so re-renders of an open panel do not report again.
  const nodeType = selectedNode ? getWorkflowNodeConcreteType(selectedNode) : null;
  const hasConfigForm = selectedNode ? getNodeRegistryEntry(selectedNode).ConfigForm !== null : false;
  useEffect(() => {
    if (!selectedNodeId || !nodeType) return;
    trackWorkflowEvent(WORKFLOW_CLIENT_EVENTS.inspectorOpened, {
      node_type: nodeType,
      has_config_form: hasConfigForm,
      is_editable: isEditable,
    });
  }, [selectedNodeId, nodeType, hasConfigForm, isEditable]);

  if (!selectedNode || !definition) return null;
  const registryEntry = getNodeRegistryEntry(selectedNode);

  const blockedReasonType = getBlockedReason(isEditable, isReadOnly, status);
  const blockedReasonMessages: Record<TBlockedReason, string> = {
    readOnly: t("workspace.workflows.edit_blocked_read_only"),
    archived: t("workspace.workflows.edit_blocked_archived"),
    active: t("workspace.workflows.edit_blocked_active"),
  };
  const blockedReason = blockedReasonType ? blockedReasonMessages[blockedReasonType] : null;
  const ConfigForm = registryEntry.ConfigForm;

  const handleChange = (nextNode: TWorkflowNode) => {
    if (!isEditable) return;
    setDefinition((current) => (current ? replaceNode(current, nextNode) : current));
  };

  return (
    <aside className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border border-slate-200 bg-white">
      <header className="flex shrink-0 items-center border-b border-slate-200 px-4 py-3">
        <h2 className="text-sm font-semibold text-slate-900">{registryEntry.title(selectedNode, t)}</h2>
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="flex flex-col gap-3 px-3 pt-3 pb-4">
          {blockedReason && (
            <Alert variant="info" size="small">
              <AlertDescription>{blockedReason}</AlertDescription>
            </Alert>
          )}
          {ConfigForm ? (
            // Keyed by node, so switching nodes remounts the form with fresh local state.
            <ConfigForm
              key={selectedNode.id}
              node={selectedNode}
              isEditable={isEditable}
              onChange={handleChange}
            />
          ) : (
            <p className="text-sm text-slate-500">{t("workspace.workflows.inspector_unsupported_node")}</p>
          )}
        </div>
      </div>
    </aside>
  );
};
