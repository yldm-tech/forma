import type { TFunction } from "i18next";
import type { TWorkflowRunSummary } from "../api";
import {
  type TWorkflowDefinition,
  type TWorkflowRunLogStatus,
  type TWorkflowStatus,
  type TWorkflowTriggerType,
  getWorkflowActionProblems,
} from "../definition";
import {
  type TWorkflowNodeFieldFocusRequest,
  type TWorkflowValidationProblem,
  workflowProblemFields,
} from "../state/editor";
import { getNodeRegistryEntry } from "./node-registry";

// Labels and badge colours for workflow and run states, and where a validation problem points, forma2's modules/workflows/lib/display.ts.

type TBadgeType = "warning" | "success" | "error" | "gray";

interface TStatusBadge {
  label: string;
  type: TBadgeType;
}

export const getWorkflowStatusBadge = (status: TWorkflowStatus, t: TFunction): TStatusBadge => {
  switch (status) {
    case "enabled":
      return { label: t("common.enabled"), type: "success" };
    case "disabled":
      return { label: t("common.disabled"), type: "gray" };
    case "archived":
      return { label: t("common.archived"), type: "gray" };
    case "draft":
      return { label: t("common.draft"), type: "gray" };
  }
};

/** A run's status badge. A run that went on past failed steps set to continue on failure completed, but not cleanly, and says so wherever it is listed. */
export const getWorkflowRunStatusBadge = (
  run: Pick<TWorkflowRunSummary, "status" | "continuedFailureCount">,
  t: TFunction
): TStatusBadge => {
  switch (run.status) {
    case "completed":
      return run.continuedFailureCount > 0
        ? { label: t("workspace.workflows.run_completed_with_failures"), type: "warning" }
        : { label: t("common.completed"), type: "success" };
    case "failed":
      return { label: t("common.failed"), type: "error" };
    case "running":
      return { label: t("common.running"), type: "warning" };
    case "canceled":
      return { label: t("common.canceled"), type: "gray" };
    case "queued":
      return { label: t("common.queued"), type: "gray" };
  }
};

export const getWorkflowRunLogStatusBadge = (status: TWorkflowRunLogStatus, t: TFunction): TStatusBadge => {
  switch (status) {
    case "succeeded":
      return { label: t("common.succeeded"), type: "success" };
    case "failed":
      return { label: t("common.failed"), type: "error" };
    case "running":
      return { label: t("common.running"), type: "warning" };
    case "skipped":
      return { label: t("common.skipped"), type: "gray" };
    case "pending":
      return { label: t("common.pending"), type: "gray" };
  }
};

export const getWorkflowTriggerTypeLabel = (triggerType: TWorkflowTriggerType, t: TFunction): string => {
  const labels: Record<TWorkflowTriggerType, string> = {
    "response.completed": t("common.response_completed"),
  };
  return labels[triggerType];
};

const NODE_FIELD_PATTERN = /^nodes\.(\d+)(?:\.|$)/;

/** Where a validation problem is, shown under its message: the title of the step or trigger its field points into, as the canvas card shows it. Null for whole-flow problems and anything that does not resolve to a node; raw paths are never shown. */
export const getWorkflowValidationProblemLocation = (
  problem: TWorkflowValidationProblem,
  definition: TWorkflowDefinition | null,
  t: TFunction
): string | null => {
  if (!definition) return null;
  const nodeMatch = NODE_FIELD_PATTERN.exec(problem.field);
  if (nodeMatch) {
    const node = definition.nodes[Number(nodeMatch[1])];
    if (!node) return null;
    return getNodeRegistryEntry(node).title(node, t);
  }
  // Only fields inside the trigger's config point at an existing node; the bare "trigger" field (trigger_missing) has nothing on the canvas.
  if (problem.field.startsWith("trigger.") && definition.trigger) {
    return getNodeRegistryEntry(definition.trigger).title(definition.trigger, t);
  }
  return null;
};

// Which trigger config path maps to which control in the trigger form.
const TRIGGER_CONFIG_FIELD_TARGETS: Record<string, string> = {
  [workflowProblemFields.triggerSurveyId]: "surveyId",
  [workflowProblemFields.triggerEndingCardIds]: "endingCardIds",
};

/** The config field a problem should send the user to, or null when it has no single field to fix (a missing trigger, a broken flow, an unnamed workflow). step_incomplete carries a step-level field, so the specific field is resolved here as the step's first problem in form order ("to", "url", "headers.0.name"). */
export const getWorkflowValidationProblemFocusTarget = (
  problem: TWorkflowValidationProblem,
  definition: TWorkflowDefinition | null
): TWorkflowNodeFieldFocusRequest | null => {
  if (!definition) return null;

  const nodeIndex = workflowProblemFields.parseNodeConfigIndex(problem.field);
  if (nodeIndex !== null) {
    const node = definition.nodes[nodeIndex];
    if (node?.type !== "action") return null;
    const [firstProblem] = getWorkflowActionProblems(node);
    return firstProblem ? { nodeId: node.id, field: firstProblem.field } : null;
  }

  const triggerField = TRIGGER_CONFIG_FIELD_TARGETS[problem.field];
  if (triggerField && definition.trigger) {
    return { nodeId: definition.trigger.id, field: triggerField };
  }
  return null;
};
