import { describe, expect, test } from "vite-plus/test";
import {
  type TWorkflowActionNode,
  type TWorkflowDefinition,
  ZWorkflowDefinition,
  ZWorkflowExecutableDefinition,
  getWorkflowActionProblems,
  isValidChatWebhookUrl,
  isValidWorkflowRequestUrl,
  isWorkflowSecretFilled,
} from "./definition";

const SURVEY_ID = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0e";
const TAG_ID = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d0f";

const chain = (...nodes: TWorkflowActionNode[]): TWorkflowDefinition => ({
  schemaVersion: 1,
  trigger: {
    id: "trigger",
    type: "trigger",
    triggerType: "response.completed",
    config: { surveyId: SURVEY_ID, endingCardIds: [] },
  },
  nodes,
  edges: nodes.map((node, index) => ({
    id: `e-${node.id}`,
    source: index === 0 ? "trigger" : nodes[index - 1]!.id,
    target: node.id,
  })),
  entryNodeId: "trigger",
});

const http = (config: Partial<Extract<TWorkflowActionNode, { actionType: "http_request" }>["config"]> = {}) =>
  ({
    id: "http",
    type: "action",
    actionType: "http_request",
    config: {
      method: "POST",
      url: "https://api.example.com/hook",
      headers: [],
      body: "",
      signingSecret: null,
      ...config,
    },
  }) satisfies TWorkflowActionNode;

describe("getWorkflowActionProblems", () => {
  test("a configured step of every kind has none, a secret counting once it is sealed or entered", () => {
    const nodes: TWorkflowActionNode[] = [
      http({
        headers: [
          { name: "X-Plain", value: "1" },
          { name: "Authorization", secret: { sealed: "abc", hint: "…ijkl" } },
          { name: "X-New", secret: { value: "entered" } },
        ],
        body: '{"answer":"{{answers.q1}}"}',
        signingSecret: { value: "whsec_abc" },
      }),
      {
        id: "slack",
        type: "action",
        actionType: "slack_message",
        config: { webhookUrl: { sealed: "x" }, message: "Hi" },
      },
      { id: "tag", type: "action", actionType: "tag_response", config: { tagId: TAG_ID } },
      {
        id: "attr",
        type: "action",
        actionType: "set_contact_attribute",
        config: { attributeKey: "plan", value: "{{answers.q1}}" },
      },
    ];
    for (const node of nodes) expect(getWorkflowActionProblems(node)).toEqual([]);
    expect(ZWorkflowExecutableDefinition.safeParse(chain(...nodes)).success).toBe(true);
  });

  test("names every field an HTTP request still needs, as the server does", () => {
    const node = http({
      method: "GET",
      url: "ftp://example.com",
      headers: [
        { name: "", value: "x" },
        { name: "bad name", value: "x" },
        { name: "Webhook-Signature", value: "x" },
        { name: "X-Token", secret: { value: "  " } },
        { name: "X-Line", value: "a\r\nInjected: 1" },
      ],
      body: '{"a":1}',
      signingSecret: {},
    });
    expect(getWorkflowActionProblems(node)).toEqual([
      { field: "url", invalid: true },
      { field: "headers.0.name", invalid: false },
      { field: "headers.1.name", invalid: true },
      { field: "headers.2.name", invalid: true },
      { field: "headers.3.secret", invalid: false },
      { field: "headers.4.value", invalid: true },
      { field: "body", invalid: true },
      { field: "signingSecret", invalid: false },
    ]);
    expect(getWorkflowActionProblems(http({ url: " ", body: "{not json" }))).toEqual([
      { field: "url", invalid: false },
      { field: "body", invalid: true },
    ]);

    const parsed = ZWorkflowExecutableDefinition.safeParse(chain(node));
    expect(parsed.success).toBe(false);
    const issues = parsed.error?.issues.map((issue) => [issue.path.join("."), issue.message]) ?? [];
    expect(issues).toContainEqual(["nodes.0.config.url", "http_request node http has an invalid url"]);
    expect(issues).toContainEqual([
      "nodes.0.config.headers.3.secret",
      "http_request node http is missing headers.3.secret",
    ]);
  });

  test("chat, tag and attribute steps", () => {
    expect(
      getWorkflowActionProblems({
        id: "d",
        type: "action",
        actionType: "discord_message",
        config: { webhookUrl: {}, message: " " },
      })
    ).toEqual([
      { field: "webhookUrl", invalid: false },
      { field: "message", invalid: false },
    ]);
    expect(
      getWorkflowActionProblems({
        id: "t",
        type: "action",
        actionType: "tag_response",
        config: { tagId: "nope" },
      })
    ).toEqual([{ field: "tagId", invalid: true }]);
    expect(
      getWorkflowActionProblems({
        id: "a",
        type: "action",
        actionType: "set_contact_attribute",
        config: { attributeKey: "", value: "" },
      })
    ).toEqual([
      { field: "attributeKey", invalid: false },
      { field: "value", invalid: false },
    ]);
  });

  test("a draft stores unfinished steps; a secret cannot carry both forms, nor a header both a value and a secret", () => {
    expect(
      ZWorkflowDefinition.safeParse(chain(http({ url: "", headers: [{ name: "", secret: {} }] }))).success
    ).toBe(true);
    expect(
      ZWorkflowDefinition.safeParse(chain(http({ signingSecret: { value: "a", sealed: "b" } }))).success
    ).toBe(false);
    expect(
      ZWorkflowDefinition.safeParse(
        chain(http({ headers: [{ name: "A", value: "1", secret: { value: "2" } }] }))
      ).success
    ).toBe(false);
    expect(ZWorkflowDefinition.safeParse(chain(http({ headers: [{ name: "A" }] }))).success).toBe(false);
  });
});

test("isWorkflowSecretFilled", () => {
  expect(isWorkflowSecretFilled({ sealed: "x" })).toBe(true);
  expect(isWorkflowSecretFilled({ value: "x" })).toBe(true);
  expect(isWorkflowSecretFilled({ value: "  " })).toBe(false);
  expect(isWorkflowSecretFilled({})).toBe(false);
  expect(isWorkflowSecretFilled(null)).toBe(false);
});

test("isValidWorkflowRequestUrl", () => {
  expect(isValidWorkflowRequestUrl("https://api.example.com/a?b=1")).toBe(true);
  expect(isValidWorkflowRequestUrl("http://10.0.0.1/a")).toBe(true);
  expect(isValidWorkflowRequestUrl("https://user:pw@example.com")).toBe(false);
  expect(isValidWorkflowRequestUrl("ftp://example.com")).toBe(false);
  expect(isValidWorkflowRequestUrl("example.com")).toBe(false);
});

test("isValidChatWebhookUrl matches each chat's own hosts only", () => {
  expect(isValidChatWebhookUrl("slack_message", "https://hooks.slack.com/services/T/B/X")).toBe(true);
  expect(isValidChatWebhookUrl("slack_message", "https://hooks.slack.com.evil.example/services/T/B/X")).toBe(
    false
  );
  expect(isValidChatWebhookUrl("slack_message", "http://hooks.slack.com/services/T/B/X")).toBe(false);
  expect(isValidChatWebhookUrl("slack_message", "https://hooks.slack.com/services/")).toBe(false);
  expect(isValidChatWebhookUrl("discord_message", "https://discord.com/api/webhooks/1/abc")).toBe(true);
  expect(isValidChatWebhookUrl("discord_message", "https://hooks.slack.com/services/T/B/X")).toBe(false);
  expect(isValidChatWebhookUrl("teams_message", "https://acme.webhook.office.com/webhookb2/x")).toBe(true);
  expect(
    isValidChatWebhookUrl("teams_message", "https://prod-01.westus.logic.azure.com:443/workflows/x/triggers")
  ).toBe(true);
  expect(isValidChatWebhookUrl("teams_message", "https://webhook.office.com/x")).toBe(false);
});
