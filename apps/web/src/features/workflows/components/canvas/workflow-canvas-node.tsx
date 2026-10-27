import { Handle, type Node, type NodeProps, Position } from "@xyflow/react";
import { useAtomValue, useSetAtom } from "jotai";
import {
  GitBranchIcon,
  GlobeIcon,
  HashIcon,
  type LucideIcon,
  MailIcon,
  MessageCircleIcon,
  MoreVerticalIcon,
  PlusIcon,
  SkipForwardIcon,
  TagIcon,
  Trash2Icon,
  TriangleAlertIcon,
  UserIcon,
  UsersIcon,
  ZapIcon,
} from "lucide-react";
import { memo } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../../components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "../../../../components/ui/dropdown-menu";
import { cn } from "../../../../lib/cn";
import { WORKFLOW_CLIENT_EVENTS, trackWorkflowEvent } from "../../lib/analytics";
import { getWorkflowActionOptions } from "../../lib/node-registry";
import {
  type TWorkflowNodeData,
  type TWorkflowNodeIcon,
  appendActionAfterNodeAtom,
  canManageWorkspaceAtom,
  canMutateCanvasAtom,
  deleteWorkflowNodeAtom,
} from "../../state/editor";

export const NODE_ICONS: Record<TWorkflowNodeIcon, LucideIcon> = {
  trigger: ZapIcon,
  ifElse: GitBranchIcon,
  email: MailIcon,
  http: GlobeIcon,
  slack: HashIcon,
  discord: MessageCircleIcon,
  teams: UsersIcon,
  tag: TagIcon,
  attribute: UserIcon,
};

/** The category colours, shared with the pickers so their icons match the canvas cards. */
export const CATEGORY_CHIP_CLASS_NAMES: Record<TWorkflowNodeData["category"], string> = {
  trigger: "bg-indigo-500 text-white",
  flow: "bg-purple-500 text-white",
  action: "bg-green-600 text-white",
};

const HANDLE_CLASS_NAMES = "!h-0 !w-0 !min-h-0 !min-w-0 !border-0 !bg-transparent !opacity-0";

/** forma2's WorkflowCanvasNode: a node's card with its title and either its summary or the reason it cannot run; actions get a delete menu, and the last step of the chain (the trigger or an action) a `+` that adds the next one. */
export const WorkflowCanvasNode = memo(
  ({ id, data, selected }: Readonly<NodeProps<Node<TWorkflowNodeData>>>) => {
    const { t } = useTranslation();
    const deleteNode = useSetAtom(deleteWorkflowNodeAtom);
    const appendAction = useSetAtom(appendActionAfterNodeAtom);
    const canMutate = useAtomValue(canMutateCanvasAtom);
    const canManage = useAtomValue(canManageWorkspaceAtom);
    const Icon = NODE_ICONS[data.icon];
    const isTrigger = data.category === "trigger";
    // Steps run in a chain, so the next one is added after the last; a branch has no form yet and gets none.
    const canAppend = data.isLeaf && canMutate && (isTrigger || data.category === "action");
    const actionOptions = getWorkflowActionOptions(t).filter((option) => canManage || !option.needsManage);

    return (
      <div className="flex flex-col items-center">
        <div
          className={cn(
            "flex w-64 items-start gap-2.5 rounded-lg border border-slate-200 bg-white px-2.5 py-2.5 shadow-sm transition-shadow hover:shadow-md",
            selected && "ring-2 ring-ink ring-offset-2 ring-offset-slate-50"
          )}>
          <Handle type="target" position={Position.Top} className={HANDLE_CLASS_NAMES} />
          <span
            className={cn(
              "flex size-8 shrink-0 items-center justify-center rounded-md",
              CATEGORY_CHIP_CLASS_NAMES[data.category]
            )}>
            <Icon className="size-4" strokeWidth={1.75} aria-hidden="true" />
          </span>
          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
            <span className="truncate text-[13px] leading-tight font-semibold text-slate-900">
              {data.title}
            </span>
            {data.issue ? (
              // The reason replaces the summary, so the user never has to guess why a card is flagged.
              <span
                className={cn(
                  "flex items-center gap-1 text-xs leading-tight",
                  data.issue.severity === "error" ? "text-red-600" : "text-amber-700"
                )}>
                <TriangleAlertIcon className="size-3 shrink-0" aria-hidden="true" />
                <span className="line-clamp-2">{data.issue.label}</span>
              </span>
            ) : (
              <span className="line-clamp-2 text-xs leading-tight text-slate-500">{data.summary}</span>
            )}
            {data.continueOnFailure ? (
              <span className="flex items-center gap-1 text-xs leading-tight text-slate-500">
                <SkipForwardIcon className="size-3 shrink-0" aria-hidden="true" />
                <span className="truncate">{t("workspace.workflows.node_continues_on_failure")}</span>
              </span>
            ) : null}
          </div>
          {!isTrigger && canMutate && (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  aria-label={t("workspace.workflows.node_actions")}
                  className="size-6"
                  onClick={(event) => event.stopPropagation()}>
                  <MoreVerticalIcon />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="motion-reduce:animate-none">
                <DropdownMenuItem
                  icon={<Trash2Icon className="size-4" />}
                  onClick={(event) => {
                    event.stopPropagation();
                    trackWorkflowEvent(WORKFLOW_CLIENT_EVENTS.nodeDeleted, { node_type: data.nodeType });
                    deleteNode(id);
                  }}>
                  {t("common.delete")}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          )}
          <Handle type="source" position={Position.Bottom} className={HANDLE_CLASS_NAMES} />
        </div>
        {canAppend && (
          // The `+` opens a picker so each step is a conscious choice.
          <div className="mt-2 flex flex-col items-center gap-1">
            <div className="h-3 w-px bg-slate-300" />
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  type="button"
                  variant="outline"
                  size="icon"
                  aria-label={t("workspace.workflows.add_action")}
                  className="size-6 rounded-full bg-white shadow-sm"
                  onClick={(event) => event.stopPropagation()}>
                  <PlusIcon />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="center" className="w-72 motion-reduce:animate-none">
                <DropdownMenuLabel>{t("common.actions")}</DropdownMenuLabel>
                <DropdownMenuSeparator />
                {actionOptions.map((option) => {
                  const OptionIcon = NODE_ICONS[option.icon];
                  return (
                    <DropdownMenuItem
                      key={option.actionType}
                      onClick={(event) => {
                        event.stopPropagation();
                        trackWorkflowEvent(WORKFLOW_CLIENT_EVENTS.actionAdded, {
                          action_type: option.actionType,
                          after_node_type: data.nodeType,
                        });
                        appendAction({ sourceNodeId: id, actionType: option.actionType });
                      }}>
                      <span
                        className={cn(
                          "flex size-6 shrink-0 items-center justify-center rounded-md",
                          CATEGORY_CHIP_CLASS_NAMES.action
                        )}>
                        <OptionIcon className="size-3.5" strokeWidth={1.75} aria-hidden="true" />
                      </span>
                      <span className="flex min-w-0 flex-col">
                        <span className="text-sm text-slate-700">{option.title}</span>
                        <span className="text-xs text-slate-500">{option.description}</span>
                      </span>
                    </DropdownMenuItem>
                  );
                })}
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        )}
      </div>
    );
  }
);

WorkflowCanvasNode.displayName = "WorkflowCanvasNode";
