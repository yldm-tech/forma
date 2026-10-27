import { z } from "zod";

// forma2's packages/workflows types: the workflow definition document, the rules that make it persistable (ZWorkflowDefinition) or executable (ZWorkflowExecutableDefinition), and the vocabulary the builder reads. The Go server enforces the same rules (apps/server/internal/workflow/definition.go); the builder runs them locally so it can flag an unready workflow before a round trip. One deliberate difference from forma2: the trigger's surveyId is a UUID, the rewrite's id format, where forma2's was a cuid2; a workflow migrated from forma2 still names its survey by the cuid2 until it is saved, and the server reads that as the id migrated from it and hands the builder the UUID. Ending card ids stay cuid2, the shape survey documents give them.

export const WORKFLOW_SCHEMA_VERSION = 1;

export const WORKFLOW_TRIGGERS = {
  RESPONSE_COMPLETED: "response.completed",
} as const;

export const WORKFLOW_ACTIONS = {
  SEND_EMAIL: "send_email",
  HTTP_REQUEST: "http_request",
  SLACK_MESSAGE: "slack_message",
  DISCORD_MESSAGE: "discord_message",
  TEAMS_MESSAGE: "teams_message",
  TAG_RESPONSE: "tag_response",
  SET_CONTACT_ATTRIBUTE: "set_contact_attribute",
} as const;
export type TWorkflowActionType = (typeof WORKFLOW_ACTIONS)[keyof typeof WORKFLOW_ACTIONS];

export const WORKFLOW_CHAT_ACTIONS = [
  WORKFLOW_ACTIONS.SLACK_MESSAGE,
  WORKFLOW_ACTIONS.DISCORD_MESSAGE,
  WORKFLOW_ACTIONS.TEAMS_MESSAGE,
] as const;
export type TWorkflowChatActionType = (typeof WORKFLOW_CHAT_ACTIONS)[number];

export const ZWorkflowStatus = z.enum(["draft", "enabled", "disabled", "archived"]);
export type TWorkflowStatus = z.infer<typeof ZWorkflowStatus>;

export const ZWorkflowSortBy = z.enum(["createdAt", "updatedAt", "name"]);
export type TWorkflowSortBy = z.infer<typeof ZWorkflowSortBy>;

export const ZWorkflowRunStatus = z.enum(["queued", "running", "completed", "failed", "canceled"]);
export type TWorkflowRunStatus = z.infer<typeof ZWorkflowRunStatus>;

export const ZWorkflowRunLogStatus = z.enum(["pending", "running", "succeeded", "failed", "skipped"]);
export type TWorkflowRunLogStatus = z.infer<typeof ZWorkflowRunLogStatus>;

export const ZWorkflowTriggerType = z.enum([WORKFLOW_TRIGGERS.RESPONSE_COMPLETED]);
export type TWorkflowTriggerType = z.infer<typeof ZWorkflowTriggerType>;

const ZWorkflowNodeUi = z
  .object({
    position: z.object({ x: z.number(), y: z.number() }).optional(),
    collapsed: z.boolean().optional(),
  })
  .catchall(z.unknown());

const ZWorkflowDataRef = z.object({
  path: z.string().min(1),
  fallback: z.string().optional(),
});

const ZWorkflowNodeBase = z.object({
  id: z.string().min(1),
  type: z.enum(["action", "if_else", "trigger"]),
  label: z.string().min(1).max(120).optional(),
  ui: ZWorkflowNodeUi.optional(),
});

// ---- Content rules ----

// An opening or closing tag: `<` (or `</`) and a tag name, with no angle bracket inside, so visible text such as "<3" is not taken for markup and matching stays linear on hostile input.
const HTML_TAG_PATTERN = /<\/?[a-zA-Z][^<>]*>/g;

// The whitespace the rich-text editor writes as entities.
const HTML_WHITESPACE_ENTITY_PATTERN = /&nbsp;|&#160;|&#xa0;/gi;

/** forma2's isBlankWorkflowRichText: whether an email body holds nothing a recipient would see once its tags and whitespace entities are gone. The editor stores an emptied body as `<p class="…"><br></p>`, never as "". */
export const isBlankWorkflowRichText = (value: string): boolean =>
  value.replaceAll(HTML_TAG_PATTERN, "").replaceAll(HTML_WHITESPACE_ENTITY_PATTERN, " ").trim().length === 0;

export const SEND_EMAIL_REQUIRED_CONTENT_FIELDS = ["to", "subject", "body"] as const;
export type TWorkflowSendEmailContentField = (typeof SEND_EMAIL_REQUIRED_CONTENT_FIELDS)[number];

/** Which required send_email fields are still blank, in form order: the one rule the executable schema, the canvas flag and the inspector's errors all read. */
export const getBlankSendEmailContentFields = (
  config: Readonly<Record<TWorkflowSendEmailContentField, string>>
): TWorkflowSendEmailContentField[] =>
  SEND_EMAIL_REQUIRED_CONTENT_FIELDS.filter((field) =>
    field === "body" ? isBlankWorkflowRichText(config[field]) : config[field].trim().length === 0
  );

const ZLiteralEmail = z.email();

/** forma2's isLiteralEmailRecipient: a `to` that is itself an address, as opposed to the id of the element whose answer holds the respondent's. */
export const isLiteralEmailRecipient = (to: string): boolean => ZLiteralEmail.safeParse(to).success;

// ---- Nodes ----

export const ZResponseCompletedTriggerConfig = z.object({
  surveyId: z.guid(),
  endingCardIds: z.array(z.cuid2()).default([]),
});

export const ZWorkflowResponseCompletedTriggerNode = ZWorkflowNodeBase.extend({
  type: z.literal("trigger"),
  triggerType: z.literal(WORKFLOW_TRIGGERS.RESPONSE_COMPLETED),
  config: ZResponseCompletedTriggerConfig,
});
export type TWorkflowResponseCompletedTriggerNode = z.infer<typeof ZWorkflowResponseCompletedTriggerNode>;

export const ZWorkflowTriggerNode = z.discriminatedUnion("triggerType", [
  ZWorkflowResponseCompletedTriggerNode,
]);
export type TWorkflowTriggerNode = z.infer<typeof ZWorkflowTriggerNode>;

// Gmail clips a message past ~102KB; RFC 5322 caps a line at 998 characters.
const MAX_BODY_LENGTH = 100_000;
const MAX_SUBJECT_LENGTH = 998;

/** The send_email action, field for field the follow-up email's. `to` is an address or the id of the survey element or hidden field whose answer holds one; `from` is vestigial and only names the Message-ID's domain, since mail always leaves from the deployment's MAIL_FROM. */
export const ZWorkflowSendEmailActionConfig = z.object({
  to: z.string(),
  from: z.email(),
  replyTo: z.array(z.email()),
  subject: z.string().max(MAX_SUBJECT_LENGTH),
  body: z.string().max(MAX_BODY_LENGTH),
  attachResponseData: z.boolean(),
  includeVariables: z.boolean().optional(),
  includeHiddenFields: z.boolean().optional(),
});

/** What every action node has besides its config: continueOnFailure lets the run go on to the next step when this one fails instead of stopping there; the failed step is still retried with the run, which then completes rather than fails. Off unless the author turns it on; the server stores it only when on. */
const ZWorkflowActionNodeBase = ZWorkflowNodeBase.extend({
  type: z.literal("action"),
  continueOnFailure: z.boolean().optional(),
});

export const ZWorkflowSendEmailActionNode = ZWorkflowActionNodeBase.extend({
  actionType: z.literal(WORKFLOW_ACTIONS.SEND_EMAIL),
  config: ZWorkflowSendEmailActionConfig,
});
export type TWorkflowSendEmailActionNode = z.infer<typeof ZWorkflowSendEmailActionNode>;

// ---- The actions after send_email (apps/server/internal/workflow/actions.go) ----

/** A value the API takes in plaintext and stores only sealed: written as `{ value }`, read back as `{ sealed, hint }`, sent back unchanged to keep it. Empty, the author has not entered it yet. */
export const ZWorkflowSecretValue = z
  .object({
    value: z.string().max(4096).optional(),
    sealed: z.string().max(8192).optional(),
    hint: z.string().max(160).optional(),
  })
  .superRefine((secret, ctx) => {
    if (secret.value !== undefined && secret.sealed) {
      ctx.addIssue({
        code: "custom",
        message: "A secret carries either a new value or its sealed form, not both",
      });
    }
  });
export type TWorkflowSecretValue = z.infer<typeof ZWorkflowSecretValue>;

/** Whether a secret holds something to send: a sealed value, or a new one the next save seals. */
export const isWorkflowSecretFilled = (secret: TWorkflowSecretValue | null | undefined): boolean =>
  Boolean(secret?.sealed) || (secret?.value ?? "").trim().length > 0;

export const WORKFLOW_HTTP_METHODS = ["GET", "POST", "PUT", "PATCH", "DELETE"] as const;
export type TWorkflowHttpMethod = (typeof WORKFLOW_HTTP_METHODS)[number];

export const MAX_WORKFLOW_HTTP_HEADERS = 20;

export const ZWorkflowHttpHeader = z
  .object({
    name: z.string().max(256),
    value: z.string().max(4096).optional(),
    secret: ZWorkflowSecretValue.optional(),
  })
  .superRefine((header, ctx) => {
    if (header.value !== undefined && header.secret !== undefined) {
      ctx.addIssue({ code: "custom", message: "A header has either a value or a secret, not both" });
    }
    if (header.value === undefined && header.secret === undefined) {
      ctx.addIssue({ code: "custom", message: "A header needs a value or a secret" });
    }
  });
export type TWorkflowHttpHeader = z.infer<typeof ZWorkflowHttpHeader>;

/** An HTTP request: the body is a JSON template whose strings may hold {{placeholders}}; a signing secret signs every request the way webhooks are signed (Standard Webhooks). */
export const ZWorkflowHttpRequestActionConfig = z.object({
  method: z.enum(WORKFLOW_HTTP_METHODS),
  url: z.string().max(2048),
  headers: z.array(ZWorkflowHttpHeader).max(MAX_WORKFLOW_HTTP_HEADERS).default([]),
  body: z.string().max(100_000).default(""),
  signingSecret: ZWorkflowSecretValue.nullable().default(null),
});

/** A Slack, Discord or Teams message: the channel's incoming-webhook URL, a credential and so a secret, and a text template. */
export const ZWorkflowChatMessageActionConfig = z.object({
  webhookUrl: ZWorkflowSecretValue,
  message: z.string().max(4000),
});

export const ZWorkflowTagResponseActionConfig = z.object({ tagId: z.string().max(64) });

export const ZWorkflowSetContactAttributeActionConfig = z.object({
  attributeKey: z.string().max(256),
  value: z.string().max(4000),
});

export const ZWorkflowHttpRequestActionNode = ZWorkflowActionNodeBase.extend({
  actionType: z.literal(WORKFLOW_ACTIONS.HTTP_REQUEST),
  config: ZWorkflowHttpRequestActionConfig,
});
export type TWorkflowHttpRequestActionNode = z.infer<typeof ZWorkflowHttpRequestActionNode>;

const chatActionNode = <TActionType extends TWorkflowChatActionType>(actionType: TActionType) =>
  ZWorkflowActionNodeBase.extend({
    actionType: z.literal(actionType),
    config: ZWorkflowChatMessageActionConfig,
  });
const ZWorkflowSlackMessageActionNode = chatActionNode(WORKFLOW_ACTIONS.SLACK_MESSAGE);
const ZWorkflowDiscordMessageActionNode = chatActionNode(WORKFLOW_ACTIONS.DISCORD_MESSAGE);
const ZWorkflowTeamsMessageActionNode = chatActionNode(WORKFLOW_ACTIONS.TEAMS_MESSAGE);
export type TWorkflowChatMessageActionNode =
  | z.infer<typeof ZWorkflowSlackMessageActionNode>
  | z.infer<typeof ZWorkflowDiscordMessageActionNode>
  | z.infer<typeof ZWorkflowTeamsMessageActionNode>;

export const ZWorkflowTagResponseActionNode = ZWorkflowActionNodeBase.extend({
  actionType: z.literal(WORKFLOW_ACTIONS.TAG_RESPONSE),
  config: ZWorkflowTagResponseActionConfig,
});
export type TWorkflowTagResponseActionNode = z.infer<typeof ZWorkflowTagResponseActionNode>;

export const ZWorkflowSetContactAttributeActionNode = ZWorkflowActionNodeBase.extend({
  actionType: z.literal(WORKFLOW_ACTIONS.SET_CONTACT_ATTRIBUTE),
  config: ZWorkflowSetContactAttributeActionConfig,
});
export type TWorkflowSetContactAttributeActionNode = z.infer<typeof ZWorkflowSetContactAttributeActionNode>;

const ZWorkflowActionNode = z.discriminatedUnion("actionType", [
  ZWorkflowSendEmailActionNode,
  ZWorkflowHttpRequestActionNode,
  ZWorkflowSlackMessageActionNode,
  ZWorkflowDiscordMessageActionNode,
  ZWorkflowTeamsMessageActionNode,
  ZWorkflowTagResponseActionNode,
  ZWorkflowSetContactAttributeActionNode,
]);
export type TWorkflowActionNode = z.infer<typeof ZWorkflowActionNode>;

export const isWorkflowChatAction = (actionType: string): actionType is TWorkflowChatActionType =>
  (WORKFLOW_CHAT_ACTIONS as readonly string[]).includes(actionType);

// The characters a header name may have (RFC 9110's token).
const HTTP_HEADER_TOKEN_PATTERN = /^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/;

/** The headers the server writes itself: the transport's, the JSON body's type and the Standard Webhooks headers. */
export const RESERVED_WORKFLOW_HTTP_HEADERS = new Set([
  "host",
  "content-length",
  "content-type",
  "transfer-encoding",
  "connection",
  "keep-alive",
  "upgrade",
  "te",
  "trailer",
  "proxy-authorization",
  "proxy-connection",
  "webhook-id",
  "webhook-timestamp",
  "webhook-signature",
]);

// Whether a value has a control character, which would split a header.
const hasControlCharacter = (value: string): boolean =>
  Array.from(value).some((char) => {
    const code = char.charCodeAt(0);
    return code < 0x20 || code === 0x7f;
  });

const parseUrl = (value: string): URL | null => {
  try {
    return new URL(value);
  } catch {
    return null;
  }
};

/** Whether a URL can be an HTTP request's target as written: absolute http or https with a host and no credentials, which belong in a header. Where it points is the server's SSRF guard's to decide. */
export const isValidWorkflowRequestUrl = (value: string): boolean => {
  const url = parseUrl(value);
  return (
    url !== null &&
    (url.protocol === "http:" || url.protocol === "https:") &&
    url.hostname !== "" &&
    url.username === "" &&
    url.password === ""
  );
};

const CHAT_WEBHOOK_HOSTS: Record<
  TWorkflowChatActionType,
  { exact: string[]; suffixes: string[]; pathPrefix: string }
> = {
  slack_message: { exact: ["hooks.slack.com"], suffixes: [], pathPrefix: "/services/" },
  discord_message: {
    exact: ["discord.com", "discordapp.com", "ptb.discord.com", "canary.discord.com"],
    suffixes: [],
    pathPrefix: "/api/webhooks/",
  },
  teams_message: {
    exact: [],
    suffixes: [".webhook.office.com", ".logic.azure.com", ".api.powerplatform.com"],
    pathPrefix: "/",
  },
};

/** Whether a URL is an https incoming webhook of the chat, the server's ValidChatWebhookURL. */
export const isValidChatWebhookUrl = (actionType: TWorkflowChatActionType, value: string): boolean => {
  const url = parseUrl(value.trim());
  const hosts = CHAT_WEBHOOK_HOSTS[actionType];
  if (!url || url.protocol !== "https:" || url.username !== "" || url.password !== "") return false;
  if (!url.pathname.startsWith(hosts.pathPrefix) || url.pathname.length <= hosts.pathPrefix.length)
    return false;
  const host = url.hostname.toLowerCase();
  return (
    hosts.exact.includes(host) ||
    hosts.suffixes.some((suffix) => host.endsWith(suffix) && host.length > suffix.length)
  );
};

const isValidJson = (value: string): boolean => {
  try {
    JSON.parse(value);
    return true;
  } catch {
    return false;
  }
};

const ZUuid = z.guid();

/** A config field that keeps an action from running: blank, or set to something it cannot run with. field is dotted within the config ("headers.0.name"). */
export type TWorkflowActionProblem = { field: string; invalid: boolean };

/** forma2's getBlankSendEmailContentFields for every action, the server's ActionProblems: what an action still needs before it can run, in form order. A secret counts once it holds a value, sealed or about to be. */
export const getWorkflowActionProblems = (node: TWorkflowActionNode): TWorkflowActionProblem[] => {
  const problems: TWorkflowActionProblem[] = [];
  const missing = (field: string) => problems.push({ field, invalid: false });
  const invalid = (field: string) => problems.push({ field, invalid: true });
  const isBlank = (value: string) => value.trim().length === 0;
  switch (node.actionType) {
    case "send_email":
      getBlankSendEmailContentFields(node.config).forEach(missing);
      break;
    case "http_request": {
      const { config } = node;
      if (isBlank(config.url)) missing("url");
      else if (!isValidWorkflowRequestUrl(config.url)) invalid("url");
      config.headers.forEach((header, index) => {
        const at = `headers.${index}.`;
        if (isBlank(header.name)) missing(`${at}name`);
        else if (
          !HTTP_HEADER_TOKEN_PATTERN.test(header.name) ||
          RESERVED_WORKFLOW_HTTP_HEADERS.has(header.name.toLowerCase())
        )
          invalid(`${at}name`);
        if (header.secret !== undefined && !isWorkflowSecretFilled(header.secret)) missing(`${at}secret`);
        else if (header.value !== undefined && hasControlCharacter(header.value)) invalid(`${at}value`);
      });
      if (!isBlank(config.body) && (config.method === "GET" || !isValidJson(config.body))) invalid("body");
      if (config.signingSecret !== null && !isWorkflowSecretFilled(config.signingSecret))
        missing("signingSecret");
      break;
    }
    case "slack_message":
    case "discord_message":
    case "teams_message":
      if (!isWorkflowSecretFilled(node.config.webhookUrl)) missing("webhookUrl");
      if (isBlank(node.config.message)) missing("message");
      break;
    case "tag_response":
      if (isBlank(node.config.tagId)) missing("tagId");
      else if (!ZUuid.safeParse(node.config.tagId).success) invalid("tagId");
      break;
    case "set_contact_attribute":
      if (isBlank(node.config.attributeKey)) missing("attributeKey");
      if (isBlank(node.config.value)) missing("value");
      break;
  }
  return problems;
};

const fieldPath = (field: string): (string | number)[] =>
  field.split(".").map((part) => (/^\d+$/.test(part) ? Number(part) : part));

const ZWorkflowConditionOperator = z.enum([
  "equals",
  "notEquals",
  "lessThan",
  "lessEqual",
  "greaterThan",
  "greaterEqual",
  "contains",
  "notContains",
  "exists",
  "notExists",
]);
type TWorkflowConditionOperator = z.infer<typeof ZWorkflowConditionOperator>;

const ZWorkflowConditionValue = z.union([z.string(), z.number(), z.boolean(), z.null(), ZWorkflowDataRef]);

const PRESENCE_OPERATORS = new Set<TWorkflowConditionOperator>(["exists", "notExists"]);

interface TWorkflowCondition {
  id: string;
  left: z.infer<typeof ZWorkflowDataRef>;
  operator: TWorkflowConditionOperator;
  right?: z.infer<typeof ZWorkflowConditionValue>;
}

interface TWorkflowConditionGroup {
  id: string;
  connector: "and" | "or";
  conditions: (TWorkflowCondition | TWorkflowConditionGroup)[];
}

const ZWorkflowCondition: z.ZodType<TWorkflowCondition> = z
  .object({
    id: z.string().min(1),
    left: ZWorkflowDataRef,
    operator: ZWorkflowConditionOperator,
    right: ZWorkflowConditionValue.optional(),
  })
  .superRefine((condition, ctx) => {
    const isPresence = PRESENCE_OPERATORS.has(condition.operator);
    if (isPresence && condition.right !== undefined) {
      ctx.addIssue({
        code: "custom",
        message: `Operator ${condition.operator} must not have a right-hand value`,
        path: ["right"],
      });
    }
    if (!isPresence && condition.right === undefined) {
      ctx.addIssue({
        code: "custom",
        message: `Operator ${condition.operator} requires a right-hand value`,
        path: ["right"],
      });
    }
  });

const ZWorkflowConditionGroup: z.ZodType<TWorkflowConditionGroup> = z.lazy(() =>
  z.object({
    id: z.string().min(1),
    connector: z.enum(["and", "or"]),
    conditions: z.array(z.union([ZWorkflowCondition, ZWorkflowConditionGroup])).min(1),
  })
);

/** A branch node: stored, never executed by this version of workflows. */
export const ZWorkflowIfElseNode = ZWorkflowNodeBase.extend({
  type: z.literal("if_else"),
  config: z.object({ condition: ZWorkflowConditionGroup }),
});
export type TWorkflowIfElseNode = z.infer<typeof ZWorkflowIfElseNode>;

const ZWorkflowChildNode = z.union([ZWorkflowActionNode, ZWorkflowIfElseNode]);
export type TWorkflowChildNode = z.infer<typeof ZWorkflowChildNode>;
export type TWorkflowNode = TWorkflowChildNode | TWorkflowTriggerNode;

const ZWorkflowEdge = z.object({
  id: z.string().min(1),
  source: z.string().min(1),
  target: z.string().min(1),
  sourceHandle: z.string().min(1).optional(),
  targetHandle: z.string().min(1).optional(),
});

const BRANCHES = new Set(["then", "else"]);

const ZWorkflowDefinitionBase = z.object({
  schemaVersion: z.number().int().positive().max(WORKFLOW_SCHEMA_VERSION).default(WORKFLOW_SCHEMA_VERSION),
  trigger: ZWorkflowTriggerNode.nullable(),
  nodes: z.array(ZWorkflowChildNode).default([]),
  edges: z.array(ZWorkflowEdge).default([]),
  entryNodeId: z.string().min(1).nullable(),
});
type TWorkflowDefinitionBase = z.infer<typeof ZWorkflowDefinitionBase>;

// forma2's validateWorkflowGraph: unique node ids, an entry point that is the trigger, edges between existing nodes, then/else handles on exactly the if_else nodes' edges, at most one edge out of the trigger, and one then and one else edge per if_else.
const validateWorkflowGraph = (definition: TWorkflowDefinitionBase, ctx: z.RefinementCtx): void => {
  const nodesById = new Map<string, TWorkflowNode>();
  if (definition.trigger) nodesById.set(definition.trigger.id, definition.trigger);
  for (const node of definition.nodes) {
    if (nodesById.has(node.id)) {
      ctx.addIssue({ code: "custom", message: `Duplicate workflow node id: ${node.id}`, path: ["nodes"] });
      continue;
    }
    nodesById.set(node.id, node);
  }

  if (definition.entryNodeId !== (definition.trigger?.id ?? null)) {
    ctx.addIssue({
      code: "custom",
      message: definition.trigger
        ? "entryNodeId must reference the workflow trigger"
        : "entryNodeId must be null while the workflow has no trigger",
      path: ["entryNodeId"],
    });
  }

  for (const [index, edge] of definition.edges.entries()) {
    if (!nodesById.has(edge.source)) {
      ctx.addIssue({
        code: "custom",
        message: `Edge source does not reference a workflow node: ${edge.source}`,
        path: ["edges", index, "source"],
      });
    }
    if (!nodesById.has(edge.target)) {
      ctx.addIssue({
        code: "custom",
        message: `Edge target does not reference a workflow node: ${edge.target}`,
        path: ["edges", index, "target"],
      });
    }
    const sourceNode = nodesById.get(edge.source);
    const isBranch = edge.sourceHandle !== undefined && BRANCHES.has(edge.sourceHandle);
    if (sourceNode?.type === "if_else" && !isBranch) {
      ctx.addIssue({
        code: "custom",
        message: "if_else edges must use then or else sourceHandle",
        path: ["edges", index, "sourceHandle"],
      });
    }
    if (sourceNode && sourceNode.type !== "if_else" && isBranch) {
      ctx.addIssue({
        code: "custom",
        message: "Only if_else nodes can use then or else sourceHandle",
        path: ["edges", index, "sourceHandle"],
      });
    }
  }

  // At most one, so a trigger-only draft stays persistable; the executable definition requires exactly one.
  const triggerEdges = definition.trigger
    ? definition.edges.filter((edge) => edge.source === definition.trigger?.id)
    : [];
  if (triggerEdges.length > 1) {
    ctx.addIssue({
      code: "custom",
      message: "A workflow can have at most one outgoing trigger edge",
      path: ["edges"],
    });
  }

  for (const node of definition.nodes) {
    if (node.type !== "if_else") continue;
    const outgoing = definition.edges.filter((edge) => edge.source === node.id);
    if (outgoing.filter((edge) => edge.sourceHandle === "then").length !== 1) {
      ctx.addIssue({
        code: "custom",
        message: `if_else node ${node.id} must have exactly one then edge`,
        path: ["edges"],
      });
    }
    if (outgoing.filter((edge) => edge.sourceHandle === "else").length !== 1) {
      ctx.addIssue({
        code: "custom",
        message: `if_else node ${node.id} must have exactly one else edge`,
        path: ["edges"],
      });
    }
  }
};

/** The document every stored workflow satisfies. A draft may have no trigger yet. */
export const ZWorkflowDefinition = ZWorkflowDefinitionBase.superRefine(validateWorkflowGraph);
export type TWorkflowDefinition = z.infer<typeof ZWorkflowDefinition>;

const ZWorkflowExecutableDefinitionBase = ZWorkflowDefinitionBase.extend({
  trigger: ZWorkflowTriggerNode,
  entryNodeId: z.string().min(1),
});
type TWorkflowExecutableDefinitionBase = z.infer<typeof ZWorkflowExecutableDefinitionBase>;

// forma2's validateExecutableGraph: exactly one edge out of the trigger, every node reachable from it, and no cycle.
const validateExecutableGraph = (
  definition: TWorkflowExecutableDefinitionBase,
  ctx: z.RefinementCtx
): void => {
  const triggerEdges = definition.edges.filter((edge) => edge.source === definition.trigger.id);
  if (triggerEdges.length !== 1) {
    ctx.addIssue({
      code: "custom",
      message: "An executable workflow must have exactly one outgoing trigger edge",
      path: ["edges"],
    });
  }

  const nodeIds = new Set<string>([definition.trigger.id, ...definition.nodes.map((node) => node.id)]);
  const targetsBySource = new Map<string, string[]>();
  for (const edge of definition.edges) {
    if (!nodeIds.has(edge.source) || !nodeIds.has(edge.target)) continue;
    targetsBySource.set(edge.source, [...(targetsBySource.get(edge.source) ?? []), edge.target]);
  }

  const reachable = new Set<string>();
  const toVisit = [definition.trigger.id];
  while (toVisit.length > 0) {
    const nodeId = toVisit.pop();
    if (!nodeId || reachable.has(nodeId)) continue;
    reachable.add(nodeId);
    toVisit.push(...(targetsBySource.get(nodeId) ?? []));
  }
  const unreachable = definition.nodes.map((node) => node.id).filter((nodeId) => !reachable.has(nodeId));
  if (unreachable.length > 0) {
    ctx.addIssue({
      code: "custom",
      message: `Executable workflow contains unreachable node ids: ${unreachable.join(", ")}`,
      path: ["nodes"],
    });
  }

  const visited = new Set<string>();
  const active = new Set<string>();
  const hasCycle = (nodeId: string): boolean => {
    if (active.has(nodeId)) return true;
    if (visited.has(nodeId)) return false;
    visited.add(nodeId);
    active.add(nodeId);
    for (const targetId of targetsBySource.get(nodeId) ?? []) {
      if (hasCycle(targetId)) return true;
    }
    active.delete(nodeId);
    return false;
  };
  for (const nodeId of nodeIds) {
    if (hasCycle(nodeId)) {
      ctx.addIssue({ code: "custom", message: "Executable workflow graph must be acyclic", path: ["edges"] });
      break;
    }
  }
};

/** The subset the runner can execute, which enable publishes: a trigger with exactly one outgoing edge, an acyclic graph with every node reachable, no if_else nodes, and every action configured enough to run (getWorkflowActionProblems). */
export const ZWorkflowExecutableDefinition = ZWorkflowExecutableDefinitionBase.superRefine(
  (definition, ctx) => {
    validateWorkflowGraph(definition, ctx);
    validateExecutableGraph(definition, ctx);
    for (const [index, node] of definition.nodes.entries()) {
      if (node.type === "if_else") {
        ctx.addIssue({
          code: "custom",
          message: "if_else nodes are not executable in this version of workflows",
          path: ["nodes", index, "type"],
        });
      }
      if (node.type === "action") {
        for (const problem of getWorkflowActionProblems(node)) {
          ctx.addIssue({
            code: "custom",
            message: `${node.actionType} node ${node.id} ${problem.invalid ? "has an invalid" : "is missing"} ${problem.field}`,
            path: ["nodes", index, "config", ...fieldPath(problem.field)],
          });
        }
      }
    }
  }
);

/** The node's concrete kind (`response.completed`, `send_email`, `if_else`). */
export const getWorkflowNodeConcreteType = (node: TWorkflowNode): string => {
  if (node.type === "trigger") return node.triggerType;
  if (node.type === "action") return node.actionType;
  return node.type;
};

/** The action with continueOnFailure turned on or off. Off removes the key, the shape the server stores, so a step turned on and off again is saved as it was. */
export const withContinueOnFailure = (node: TWorkflowActionNode, on: boolean): TWorkflowActionNode => {
  const next = { ...node };
  if (on) next.continueOnFailure = true;
  else delete next.continueOnFailure;
  return next;
};
