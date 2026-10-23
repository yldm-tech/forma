import { createId } from "@paralleldrive/cuid2";
import {
  TSurveyBlockLogic,
  TSurveyBlockLogicAction,
  TSurveyBlockLogicActionObjective,
} from "@forma/types/surveys/blocks";
import { TConditionGroup, TSingleCondition } from "@forma/types/surveys/logic";

type TCondition = TSingleCondition | TConditionGroup;

export const isConditionGroup = (condition: TCondition): condition is TConditionGroup => {
  return (condition as TConditionGroup).connector !== undefined;
};

export const duplicateLogicItem = (logicItem: TSurveyBlockLogic): TSurveyBlockLogic => {
  const duplicateConditionGroup = (group: TConditionGroup): TConditionGroup => {
    return {
      ...group,
      id: createId(),
      conditions: group.conditions.map((condition) => {
        if (isConditionGroup(condition)) {
          return duplicateConditionGroup(condition);
        } else {
          return duplicateCondition(condition);
        }
      }),
    };
  };

  const duplicateCondition = (condition: TSingleCondition): TSingleCondition => {
    return {
      ...condition,
      id: createId(),
    };
  };

  const duplicateAction = (action: TSurveyBlockLogicAction): TSurveyBlockLogicAction => {
    return {
      ...action,
      id: createId(),
    };
  };

  return {
    ...logicItem,
    id: createId(),
    conditions: duplicateConditionGroup(logicItem.conditions),
    actions: logicItem.actions.map(duplicateAction),
  };
};

export const addConditionBelow = (
  group: TConditionGroup,
  resourceId: string,
  condition: TSingleCondition
) => {
  for (let i = 0; i < group.conditions.length; i++) {
    const item = group.conditions[i];
    if (!item) continue;

    if (isConditionGroup(item)) {
      if (item.id === resourceId) {
        group.conditions.splice(i + 1, 0, condition);
        break;
      } else {
        addConditionBelow(item, resourceId, condition);
      }
    } else {
      if (item.id === resourceId) {
        group.conditions.splice(i + 1, 0, condition);
        break;
      }
    }
  }
};

export const toggleGroupConnector = (group: TConditionGroup, resourceId: string) => {
  if (group.id === resourceId) {
    group.connector = group.connector === "and" ? "or" : "and";
    return;
  }

  for (const condition of group.conditions) {
    if (condition.connector) {
      toggleGroupConnector(condition, resourceId);
    }
  }
};

export const removeCondition = (group: TConditionGroup, resourceId: string): boolean => {
  for (let i = group.conditions.length - 1; i >= 0; i--) {
    const item = group.conditions[i];
    if (!item) continue;

    if (item.id === resourceId) {
      group.conditions.splice(i, 1);
      cleanupGroup(group);
      return true;
    }

    if (isConditionGroup(item) && removeCondition(item, resourceId)) {
      cleanupGroup(group);
      return true;
    }
  }

  return false;
};

const cleanupGroup = (group: TConditionGroup) => {
  // Remove empty condition groups first
  for (let i = group.conditions.length - 1; i >= 0; i--) {
    const condition = group.conditions[i];
    if (!condition) continue;
    if (isConditionGroup(condition)) {
      cleanupGroup(condition);

      // Remove if empty after cleanup
      if (condition.conditions.length === 0) {
        group.conditions.splice(i, 1);
      }
    }
  }

  // Flatten if group has only one condition and it's a condition group
  const [only] = group.conditions;
  if (group.conditions.length === 1 && only && isConditionGroup(only)) {
    group.connector = only.connector || "and";
    group.conditions = only.conditions;
  }
};

export const deleteEmptyGroups = (group: TConditionGroup) => {
  cleanupGroup(group);
};

export const duplicateCondition = (group: TConditionGroup, resourceId: string) => {
  for (let i = 0; i < group.conditions.length; i++) {
    const item = group.conditions[i];
    if (!item) continue;

    if (item.id === resourceId) {
      const newItem: TCondition = {
        ...item,
        id: createId(),
      };
      group.conditions.splice(i + 1, 0, newItem);
      return;
    }

    if (item.connector) {
      duplicateCondition(item, resourceId);
    }
  }
};

export const createGroupFromResource = (group: TConditionGroup, resourceId: string) => {
  for (let i = 0; i < group.conditions.length; i++) {
    const item = group.conditions[i];
    if (!item) continue;

    if (item.id === resourceId) {
      const newGroup: TConditionGroup = {
        id: createId(),
        connector: "and",
        conditions: [item],
      };
      group.conditions[i] = newGroup;
      group.connector = group.connector ?? "and";
      return;
    }

    if (isConditionGroup(item)) {
      createGroupFromResource(item, resourceId);
    }
  }
};

export const updateCondition = (
  group: TConditionGroup,
  resourceId: string,
  condition: Partial<TSingleCondition>
) => {
  for (let i = 0; i < group.conditions.length; i++) {
    const item = group.conditions[i];
    if (!item) continue;

    if (item.id === resourceId && !("connector" in item)) {
      group.conditions[i] = { ...item, ...condition } as TSingleCondition;
      return;
    }

    if (isConditionGroup(item)) {
      updateCondition(item, resourceId, condition);
    }
  }
};

export const getUpdatedActionBody = (
  action: TSurveyBlockLogicAction,
  objective: TSurveyBlockLogicActionObjective
): TSurveyBlockLogicAction => {
  if (objective === action.objective) return action;
  switch (objective) {
    case "calculate":
      return {
        id: action.id,
        objective: "calculate",
        variableId: "",
        operator: "assign",
        value: { type: "static", value: "" },
      };
    case "requireAnswer":
      return {
        id: action.id,
        objective: "requireAnswer",
        target: "",
      };
    case "jumpToBlock":
      return {
        id: action.id,
        objective: "jumpToBlock",
        target: "",
      };
    default:
      return action;
  }
};
