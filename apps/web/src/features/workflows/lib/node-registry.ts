import type { TFunction } from "i18next";
import type { ComponentType } from "react";
import { WorkflowChatMessageForm } from "../components/inspector/workflow-chat-message-form";
import { WorkflowContactAttributeForm } from "../components/inspector/workflow-contact-attribute-form";
import { WorkflowEmailActionForm } from "../components/inspector/workflow-email-action-form";
import { WorkflowHttpRequestForm } from "../components/inspector/workflow-http-request-form";
import { WorkflowTagResponseForm } from "../components/inspector/workflow-tag-response-form";
import { WorkflowTriggerForm } from "../components/inspector/workflow-trigger-form";
import {
  type TWorkflowActionNode,
  type TWorkflowActionType,
  type TWorkflowNode,
  WORKFLOW_ACTIONS,
  type WORKFLOW_TRIGGERS,
  getWorkflowActionProblems,
} from "../definition";
import type { TWorkflowNodeCategory, TWorkflowNodeIcon } from "../state/editor";

// What the canvas and the inspector know about each kind of node, forma2's modules/workflows/lib/node-registry.ts.

export type TWorkflowNodeRegistryKind =
  | `trigger:${(typeof WORKFLOW_TRIGGERS)[keyof typeof WORKFLOW_TRIGGERS]}`
  | `action:${(typeof WORKFLOW_ACTIONS)[keyof typeof WORKFLOW_ACTIONS]}`
  | "if_else";

export interface TWorkflowNodeFormProps<TNode extends TWorkflowNode> {
  node: TNode;
  isEditable: boolean;
  onChange: (next: TNode) => void;
}

export interface TWorkflowNodeRegistryEntry {
  kind: TWorkflowNodeRegistryKind;
  category: TWorkflowNodeCategory;
  icon: TWorkflowNodeIcon;
  title: (node: TWorkflowNode, t: TFunction) => string;
  summary: (node: TWorkflowNode, t: TFunction) => string;
  /** Stored erased to TWorkflowNode; the inspector renders it only for its own kind. */
  ConfigForm: ComponentType<TWorkflowNodeFormProps<TWorkflowNode>> | null;
}

const eraseForm = <TNode extends TWorkflowNode>(
  Form: ComponentType<TWorkflowNodeFormProps<TNode>>
): ComponentType<TWorkflowNodeFormProps<TWorkflowNode>> =>
  Form as unknown as ComponentType<TWorkflowNodeFormProps<TWorkflowNode>>;

const responseCompletedTriggerEntry: TWorkflowNodeRegistryEntry = {
  kind: "trigger:response.completed",
  category: "trigger",
  icon: "trigger",
  title: (_node, t) => t("workspace.workflows.response_completed"),
  summary: (node, t) => {
    if (node.type !== "trigger") return "";
    return node.config.endingCardIds.length > 0
      ? t("workspace.workflows.trigger_summary_ending_cards", { count: node.config.endingCardIds.length })
      : t("workspace.workflows.trigger_summary_all_endings");
  },
  ConfigForm: eraseForm(WorkflowTriggerForm),
};

const sendEmailActionEntry: TWorkflowNodeRegistryEntry = {
  kind: "action:send_email",
  category: "action",
  icon: "email",
  title: (node, t) => node.label ?? t("workspace.workflows.send_email"),
  summary: (node, t) => {
    if (node.type !== "action" || node.actionType !== WORKFLOW_ACTIONS.SEND_EMAIL) return "";
    return node.config.to
      ? t("workspace.workflows.send_email_summary", { to: node.config.to })
      : t("workspace.workflows.send_email_unconfigured");
  },
  ConfigForm: eraseForm(WorkflowEmailActionForm),
};

// The summary of a step after send_email: what it does once configured, or that it is not yet.
const actionSummary =
  (configured: (node: TWorkflowActionNode, t: TFunction) => string) =>
  (node: TWorkflowNode, t: TFunction): string => {
    if (node.type !== "action") return "";
    return getWorkflowActionProblems(node).length > 0
      ? t("workspace.workflows.action_unconfigured")
      : configured(node, t);
  };

const hostOf = (value: string): string => {
  try {
    return new URL(value).host;
  } catch {
    return value;
  }
};

const httpRequestActionEntry: TWorkflowNodeRegistryEntry = {
  kind: "action:http_request",
  category: "action",
  icon: "http",
  title: (node, t) => node.label ?? t("workspace.workflows.http_request"),
  summary: actionSummary((node, t) =>
    node.actionType === WORKFLOW_ACTIONS.HTTP_REQUEST
      ? t("workspace.workflows.http_request_summary", {
          method: node.config.method,
          host: hostOf(node.config.url),
        })
      : ""
  ),
  ConfigForm: eraseForm(WorkflowHttpRequestForm),
};

const chatActionEntry = (
  actionType: "slack_message" | "discord_message" | "teams_message",
  icon: TWorkflowNodeIcon,
  title: (t: TFunction) => string
): TWorkflowNodeRegistryEntry => ({
  kind: `action:${actionType}`,
  category: "action",
  icon,
  title: (node, t) => node.label ?? title(t),
  summary: actionSummary((_node, t) => t("workspace.workflows.chat_message_summary")),
  ConfigForm: eraseForm(WorkflowChatMessageForm),
});

const tagResponseActionEntry: TWorkflowNodeRegistryEntry = {
  kind: "action:tag_response",
  category: "action",
  icon: "tag",
  title: (node, t) => node.label ?? t("workspace.workflows.tag_response"),
  summary: actionSummary((_node, t) => t("workspace.workflows.tag_response_summary")),
  ConfigForm: eraseForm(WorkflowTagResponseForm),
};

const setContactAttributeActionEntry: TWorkflowNodeRegistryEntry = {
  kind: "action:set_contact_attribute",
  category: "action",
  icon: "attribute",
  title: (node, t) => node.label ?? t("workspace.workflows.set_contact_attribute"),
  summary: actionSummary((node, t) =>
    node.actionType === WORKFLOW_ACTIONS.SET_CONTACT_ATTRIBUTE
      ? t("workspace.workflows.set_contact_attribute_summary", { key: node.config.attributeKey })
      : ""
  ),
  ConfigForm: eraseForm(WorkflowContactAttributeForm),
};

const ifElseEntry: TWorkflowNodeRegistryEntry = {
  kind: "if_else",
  category: "flow",
  icon: "ifElse",
  title: (_node, t) => t("workspace.workflows.if_else"),
  summary: (_node, t) => t("workspace.workflows.if_else_summary"),
  ConfigForm: null,
};

const REGISTRY: Record<TWorkflowNodeRegistryKind, TWorkflowNodeRegistryEntry> = {
  "trigger:response.completed": responseCompletedTriggerEntry,
  "action:send_email": sendEmailActionEntry,
  "action:http_request": httpRequestActionEntry,
  "action:slack_message": chatActionEntry("slack_message", "slack", (t) =>
    t("workspace.workflows.slack_message")
  ),
  "action:discord_message": chatActionEntry("discord_message", "discord", (t) =>
    t("workspace.workflows.discord_message")
  ),
  "action:teams_message": chatActionEntry("teams_message", "teams", (t) =>
    t("workspace.workflows.teams_message")
  ),
  "action:tag_response": tagResponseActionEntry,
  "action:set_contact_attribute": setContactAttributeActionEntry,
  if_else: ifElseEntry,
};

/** An action the add-step picker offers: its type, its card's icon and the picker's title and description. */
export interface TWorkflowActionOption {
  actionType: TWorkflowActionType;
  icon: TWorkflowNodeIcon;
  title: string;
  description: string;
  /** Offered only to members who can manage the workspace (an HTTP request). */
  needsManage: boolean;
}

/** The actions the add-step picker lists, in the order it lists them. */
export const getWorkflowActionOptions = (t: TFunction): TWorkflowActionOption[] => [
  {
    actionType: WORKFLOW_ACTIONS.SEND_EMAIL,
    icon: "email",
    title: t("workspace.workflows.send_email"),
    description: t("workspace.workflows.send_email_description"),
    needsManage: false,
  },
  {
    actionType: WORKFLOW_ACTIONS.SLACK_MESSAGE,
    icon: "slack",
    title: t("workspace.workflows.slack_message"),
    description: t("workspace.workflows.slack_message_description"),
    needsManage: false,
  },
  {
    actionType: WORKFLOW_ACTIONS.DISCORD_MESSAGE,
    icon: "discord",
    title: t("workspace.workflows.discord_message"),
    description: t("workspace.workflows.discord_message_description"),
    needsManage: false,
  },
  {
    actionType: WORKFLOW_ACTIONS.TEAMS_MESSAGE,
    icon: "teams",
    title: t("workspace.workflows.teams_message"),
    description: t("workspace.workflows.teams_message_description"),
    needsManage: false,
  },
  {
    actionType: WORKFLOW_ACTIONS.TAG_RESPONSE,
    icon: "tag",
    title: t("workspace.workflows.tag_response"),
    description: t("workspace.workflows.tag_response_description"),
    needsManage: false,
  },
  {
    actionType: WORKFLOW_ACTIONS.SET_CONTACT_ATTRIBUTE,
    icon: "attribute",
    title: t("workspace.workflows.set_contact_attribute"),
    description: t("workspace.workflows.set_contact_attribute_description"),
    needsManage: false,
  },
  {
    actionType: WORKFLOW_ACTIONS.HTTP_REQUEST,
    icon: "http",
    title: t("workspace.workflows.http_request"),
    description: t("workspace.workflows.http_request_description"),
    needsManage: true,
  },
];

export const getNodeRegistryKind = (node: TWorkflowNode): TWorkflowNodeRegistryKind => {
  if (node.type === "trigger") return `trigger:${node.triggerType}`;
  if (node.type === "if_else") return "if_else";
  return `action:${node.actionType}`;
};

export const getNodeRegistryEntry = (node: TWorkflowNode): TWorkflowNodeRegistryEntry =>
  REGISTRY[getNodeRegistryKind(node)];

/** A run step's type as the member reads it, the title the add-step picker gives that action; a type this version does not know as stored. */
export const getWorkflowStepTypeTitle = (stepType: string, t: TFunction): string =>
  getWorkflowActionOptions(t).find((option) => option.actionType === stepType)?.title ?? stepType;

/** The label the step's author gave it, which the runner records in the step's input; null for a step without one, or one recorded before labels were. */
export const getWorkflowStepLabel = (input: Record<string, unknown>): string | null =>
  typeof input.label === "string" && input.label.trim() !== "" ? input.label : null;
