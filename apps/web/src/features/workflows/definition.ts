import { z } from "zod";

// forma2's packages/workflows types: the workflow definition document, the rules that make it persistable (ZWorkflowDefinition) or executable (ZWorkflowExecutableDefinition), and the vocabulary the builder reads. The Go server enforces the same rules (apps/server/internal/workflow/definition.go); the builder runs them locally so it can flag an unready workflow before a round trip. One deliberate difference from forma2: the trigger's surveyId is a UUID, the rewrite's id format, where forma2's was a cuid2; a workflow migrated from forma2 still names its survey by the cuid2 until it is saved, and the server reads that as the id migrated from it and hands the builder the UUID. Ending card ids stay cuid2, the shape survey documents give them.

export const WORKFLOW_SCHEMA_VERSION = 1;

export const WORKFLOW_TRIGGERS = {
  RESPONSE_COMPLETED: "response.completed",
} as const;

export const WORKFLOW_ACTIONS = {
  SEND_EMAIL: "send_email",
} as const;

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

export const ZWorkflowSendEmailActionNode = ZWorkflowNodeBase.extend({
  type: z.literal("action"),
  actionType: z.literal(WORKFLOW_ACTIONS.SEND_EMAIL),
  config: ZWorkflowSendEmailActionConfig,
});
export type TWorkflowSendEmailActionNode = z.infer<typeof ZWorkflowSendEmailActionNode>;

const ZWorkflowActionNode = z.discriminatedUnion("actionType", [ZWorkflowSendEmailActionNode]);

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

/** The subset the runner can execute, which enable publishes: a trigger with exactly one outgoing edge, an acyclic graph with every node reachable, no if_else nodes, and a to, subject and body in every send_email action. */
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
        for (const field of getBlankSendEmailContentFields(node.config)) {
          ctx.addIssue({
            code: "custom",
            message: `send_email node ${node.id} is missing ${field}`,
            path: ["nodes", index, "config", field],
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
