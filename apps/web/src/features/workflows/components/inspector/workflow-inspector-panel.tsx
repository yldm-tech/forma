import { useAtomValue } from "jotai";
import { cn } from "../../../../lib/cn";
import { isWorkflowInspectorCollapsedAtom, isWorkflowNodeConfigModalOpenAtom } from "../../state/editor";
import { WorkflowNodeConfigPanel } from "./workflow-node-config-panel";

/** The inspector column: the selected node's config, collapsing away while no node is open. */
export const WorkflowInspectorPanel = ({ isEditingNode }: Readonly<{ isEditingNode: boolean }>) => {
  const isCollapsed = useAtomValue(isWorkflowInspectorCollapsedAtom);
  const isNodeConfigOpen = useAtomValue(isWorkflowNodeConfigModalOpenAtom);
  const isVisible = isNodeConfigOpen && !isCollapsed;
  return (
    <div
      aria-hidden={!isVisible}
      className={cn(
        "min-h-0 shrink-0 overflow-hidden transition-[width,opacity] duration-150 ease-in-out",
        isVisible ? "w-[360px] opacity-100" : "w-0 opacity-0"
      )}>
      {/* Mounted only while open: left mounted, the fixed-width content would widen the document's scroll width from inside the collapsed column. */}
      {isVisible && (
        <div className="flex h-full w-[360px] flex-col gap-3">
          <WorkflowNodeConfigPanel isEditable={isEditingNode} />
        </div>
      )}
    </div>
  );
};
