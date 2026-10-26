import type { TFunction } from "i18next";
import { describe, expect, test } from "vite-plus/test";
import type {
  TWorkflowIfElseNode,
  TWorkflowNode,
  TWorkflowResponseCompletedTriggerNode,
  TWorkflowSendEmailActionNode,
} from "../definition";
import { getNodeRegistryEntry, getNodeRegistryKind, getWorkflowActionOptions } from "./node-registry";

const t = ((key: string, options?: Record<string, unknown>) =>
  options ? `${key}:${JSON.stringify(options)}` : key) as unknown as TFunction;

const triggerNode: TWorkflowResponseCompletedTriggerNode = {
  id: "trigger-1",
  type: "trigger",
  triggerType: "response.completed",
  config: { surveyId: "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0e", endingCardIds: [] },
};

const triggerNodeWithEndings: TWorkflowResponseCompletedTriggerNode = {
  ...triggerNode,
  config: { surveyId: "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0e", endingCardIds: ["ending-1", "ending-2"] },
};

const sendEmailNode: TWorkflowSendEmailActionNode = {
  id: "action-1",
  type: "action",
  actionType: "send_email",
  config: {
    to: "respondent@example.com",
    from: "team@example.com",
    replyTo: [],
    subject: "Subject",
    body: "Body",
    attachResponseData: false,
  },
};

const ifElseNode: TWorkflowIfElseNode = {
  id: "if-1",
  type: "if_else",
  config: { condition: { connector: "and", conditions: [] } },
} as unknown as TWorkflowIfElseNode;

describe("getNodeRegistryKind", () => {
  test.each([
    [triggerNode, "trigger:response.completed"],
    [sendEmailNode, "action:send_email"],
    [ifElseNode, "if_else"],
  ] as const)("maps node to its kind", (node, expectedKind) => {
    expect(getNodeRegistryKind(node)).toBe(expectedKind);
  });
});

describe("getNodeRegistryEntry", () => {
  test("trigger entry exposes title and summary", () => {
    const entry = getNodeRegistryEntry(triggerNode);
    expect(entry.category).toBe("trigger");
    expect(entry.icon).toBe("trigger");
    expect(entry.title(triggerNode, t)).toBe("workspace.workflows.response_completed");
    expect(entry.summary(triggerNode, t)).toBe("workspace.workflows.trigger_summary_all_endings");
  });

  test("trigger summary includes ending-card count when configured", () => {
    const entry = getNodeRegistryEntry(triggerNodeWithEndings);
    expect(entry.summary(triggerNodeWithEndings, t)).toContain('"count":2');
  });

  test("send-email entry includes recipient in summary", () => {
    const entry = getNodeRegistryEntry(sendEmailNode);
    expect(entry.category).toBe("action");
    expect(entry.icon).toBe("email");
    expect(entry.title(sendEmailNode, t)).toBe("workspace.workflows.send_email");
    expect(entry.summary(sendEmailNode, t)).toContain("respondent@example.com");
  });

  test("send-email entry uses label override when present", () => {
    const labelled = { ...sendEmailNode, label: "Custom send" };
    expect(getNodeRegistryEntry(labelled).title(labelled, t)).toBe("Custom send");
  });

  test("send-email entry falls back to the unconfigured summary when `to` is blank", () => {
    const unconfigured = { ...sendEmailNode, config: { ...sendEmailNode.config, to: "" } };
    expect(getNodeRegistryEntry(unconfigured).summary(unconfigured, t)).toBe(
      "workspace.workflows.send_email_unconfigured"
    );
  });

  test("if-else entry exposes flow category + summary key", () => {
    const entry = getNodeRegistryEntry(ifElseNode);
    expect(entry.category).toBe("flow");
    expect(entry.icon).toBe("ifElse");
    expect(entry.title(ifElseNode, t)).toBe("workspace.workflows.if_else");
    expect(entry.summary(ifElseNode, t)).toBe("workspace.workflows.if_else_summary");
    expect(entry.ConfigForm).toBeNull();
  });
});

describe("the actions after send_email", () => {
  const httpNode: TWorkflowNode = {
    id: "http-1",
    type: "action",
    actionType: "http_request",
    config: {
      method: "PUT",
      url: "https://api.example.com/hooks/1",
      headers: [],
      body: "",
      signingSecret: null,
    },
  };

  test("each has its own kind, icon and form, and its summary says whether it is configured", () => {
    expect(getNodeRegistryKind(httpNode)).toBe("action:http_request");
    const entry = getNodeRegistryEntry(httpNode);
    expect(entry.icon).toBe("http");
    expect(entry.ConfigForm).not.toBeNull();
    expect(entry.title(httpNode, t)).toBe("workspace.workflows.http_request");
    expect(entry.summary(httpNode, t)).toBe(
      'workspace.workflows.http_request_summary:{"method":"PUT","host":"api.example.com"}'
    );
    const unconfigured: TWorkflowNode = {
      id: "s",
      type: "action",
      actionType: "slack_message",
      config: { webhookUrl: {}, message: "" },
    };
    expect(getNodeRegistryEntry(unconfigured).icon).toBe("slack");
    expect(getNodeRegistryEntry(unconfigured).summary(unconfigured, t)).toBe(
      "workspace.workflows.action_unconfigured"
    );
  });

  test("the picker offers every action, the HTTP request only to managers", () => {
    const options = getWorkflowActionOptions(t);
    expect(options.map((option) => option.actionType)).toEqual([
      "send_email",
      "slack_message",
      "discord_message",
      "teams_message",
      "tag_response",
      "set_contact_attribute",
      "http_request",
    ]);
    expect(options.filter((option) => option.needsManage).map((option) => option.actionType)).toEqual([
      "http_request",
    ]);
  });
});
