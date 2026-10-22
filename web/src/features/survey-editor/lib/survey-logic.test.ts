import { describe, expect, test, vi } from "vite-plus/test";
import { TSurveyBlockLogic, TSurveyBlockLogicAction } from "@forma/types/surveys/blocks";
import { TConditionGroup, TSingleCondition } from "@forma/types/surveys/logic";
import { TSurveyLogicAction } from "@forma/types/surveys/types";
import {
  addConditionBelow,
  createGroupFromResource,
  deleteEmptyGroups,
  duplicateCondition,
  duplicateLogicItem,
  getUpdatedActionBody,
  removeCondition,
  toggleGroupConnector,
  updateCondition,
} from "./survey-logic";

vi.mock("../../../lib/survey/i18n", () => ({
  getLocalizedValue: (label: { default: string }) => label.default,
}));
vi.mock("@paralleldrive/cuid2", () => ({
  createId: () => "fixed-id",
}));

describe("surveyLogic", () => {
  /** Overridden by several cases below; the rows have to match, since they are what is read. */
  const simpleGroup = (): TConditionGroup => ({
    id: "g1",
    connector: "and",
    conditions: [
      {
        id: "c1",
        leftOperand: { type: "hiddenField", value: "f1" },
        operator: "equals",
        rightOperand: { type: "static", value: "v1" },
      },
      {
        id: "c2",
        leftOperand: { type: "hiddenField", value: "f2" },
        operator: "equals",
        rightOperand: { type: "static", value: "v2" },
      },
    ],
  });

  test("duplicateLogicItem duplicates IDs recursively", () => {
    const logic: TSurveyBlockLogic = {
      id: "L1",
      conditions: simpleGroup(),
      actions: [{ id: "A1", objective: "requireAnswer", target: "q1" }],
    };
    const dup = duplicateLogicItem(logic);
    expect(dup.id).toBe("fixed-id");
    expect(dup.conditions.id).toBe("fixed-id");
    expect(dup.actions[0]!.id).toBe("fixed-id");
  });

  test("addConditionBelow inserts after matched id", () => {
    const group = simpleGroup();
    const newCond: TSingleCondition = {
      id: "new",
      leftOperand: { type: "hiddenField", value: "x" },
      operator: "equals",
      rightOperand: { type: "static", value: "y" },
    };
    addConditionBelow(group, "c1", newCond);
    expect(group.conditions[1]).toEqual(newCond);
  });

  test("toggleGroupConnector flips connector", () => {
    const g = simpleGroup();
    toggleGroupConnector(g, "g1");
    expect(g.connector).toBe("or");
    toggleGroupConnector(g, "g1");
    expect(g.connector).toBe("and");
  });

  test("removeCondition deletes the condition and cleans empty groups", () => {
    const group: TConditionGroup = {
      id: "root",
      connector: "and",
      conditions: [
        {
          id: "c",
          leftOperand: { type: "hiddenField", value: "f" },
          operator: "equals",
          rightOperand: { type: "static", value: "" },
        },
      ],
    };
    const result = removeCondition(group, "c");
    expect(result).toBe(true);
    expect(group.conditions).toHaveLength(0);
  });

  test("duplicateCondition clones a condition in place", () => {
    const group = simpleGroup();
    duplicateCondition(group, "c1");
    expect(group.conditions[1]!.id).toBe("fixed-id");
  });

  test("deleteEmptyGroups removes nested empty groups", () => {
    const nested: TConditionGroup = { id: "n", connector: "and", conditions: [] };
    const root: TConditionGroup = { id: "r", connector: "and", conditions: [nested] };
    deleteEmptyGroups(root);
    expect(root.conditions).toHaveLength(0);
  });

  test("createGroupFromResource wraps item in new group", () => {
    const group = simpleGroup();
    createGroupFromResource(group, "c1");
    const g = group.conditions[0] as TConditionGroup;
    expect(g.conditions[0]!.id).toBe("c1");
    expect(g.connector).toBe("and");
  });

  test("updateCondition merges in partial changes", () => {
    const group = simpleGroup();
    updateCondition(group, "c1", { operator: "contains", rightOperand: { type: "static", value: "z" } });
    const updated = group.conditions.find((c) => c.id === "c1") as TSingleCondition;
    expect(updated?.operator).toBe("contains");
    expect(updated?.rightOperand?.value).toBe("z");
  });

  test("getUpdatedActionBody returns new action bodies correctly", () => {
    const base: TSurveyBlockLogicAction = { id: "A", objective: "requireAnswer", target: "q" };
    const calc = getUpdatedActionBody(base, "calculate");
    expect(calc.objective).toBe("calculate");
    const req = getUpdatedActionBody(calc, "requireAnswer");
    expect(req.objective).toBe("requireAnswer");
    const jump = getUpdatedActionBody(req, "jumpToBlock");
    expect(jump.objective).toBe("jumpToBlock");
  });

  test("getUpdatedActionBody returns same action if objective matches", () => {
    const base: TSurveyLogicAction = { id: "A", objective: "requireAnswer", target: "q" };
    expect(getUpdatedActionBody(base, "requireAnswer")).toBe(base);
  });

  test("group/condition manipulation functions handle missing resourceId", () => {
    const group = simpleGroup();
    addConditionBelow(group, "notfound", {
      id: "x",
      leftOperand: { type: "hiddenField", value: "a" },
      operator: "equals",
      rightOperand: { type: "static", value: "b" },
    });
    expect(group.conditions.length).toBe(2);
    toggleGroupConnector(group, "notfound");
    expect(group.connector).toBe("and");
    const result = removeCondition(group, "notfound");
    expect(result).toBe(false);
    expect(group.conditions.length).toBe(2);
    duplicateCondition(group, "notfound");
    expect(group.conditions.length).toBe(2);
    createGroupFromResource(group, "notfound");
    expect(group.conditions.length).toBe(2);
    updateCondition(group, "notfound", { operator: "equals" });
    expect(group.conditions.length).toBe(2);
  });

  test("removeCondition returns false when condition not found in nested groups", () => {
    const nestedGroup: TConditionGroup = {
      id: "nested",
      connector: "and",
      conditions: [
        {
          id: "nestedC1",
          leftOperand: { type: "hiddenField", value: "nf1" },
          operator: "equals",
          rightOperand: { type: "static", value: "nv1" },
        },
      ],
    };

    const group: TConditionGroup = {
      id: "parent",
      connector: "and",
      conditions: [nestedGroup],
    };

    const result = removeCondition(group, "nonexistent");
    expect(result).toBe(false);
    expect(group.conditions).toHaveLength(1);
  });

  test("removeCondition successfully removes from nested groups and cleans up", () => {
    const nestedGroup: TConditionGroup = {
      id: "nested",
      connector: "and",
      conditions: [
        {
          id: "nestedC1",
          leftOperand: { type: "hiddenField", value: "nf1" },
          operator: "equals",
          rightOperand: { type: "static", value: "nv1" },
        },
        {
          id: "nestedC2",
          leftOperand: { type: "hiddenField", value: "nf2" },
          operator: "equals",
          rightOperand: { type: "static", value: "nv2" },
        },
      ],
    };

    const otherCondition: TSingleCondition = {
      id: "otherCondition",
      leftOperand: { type: "hiddenField", value: "other" },
      operator: "equals",
      rightOperand: { type: "static", value: "value" },
    };

    const group: TConditionGroup = {
      id: "parent",
      connector: "and",
      conditions: [nestedGroup, otherCondition],
    };

    const result = removeCondition(group, "nestedC1");
    expect(result).toBe(true);
    expect(group.conditions).toHaveLength(2);
    expect((group.conditions[0] as TConditionGroup).conditions).toHaveLength(1);
    expect((group.conditions[0] as TConditionGroup).conditions[0]!.id).toBe("nestedC2");
    expect(group.conditions[1]!.id).toBe("otherCondition");
  });

  test("removeCondition flattens group when nested group has only one condition left", () => {
    const deeplyNestedGroup: TConditionGroup = {
      id: "deepNested",
      connector: "or",
      conditions: [
        {
          id: "deepC1",
          leftOperand: { type: "hiddenField", value: "df1" },
          operator: "equals",
          rightOperand: { type: "static", value: "dv1" },
        },
      ],
    };

    const nestedGroup: TConditionGroup = {
      id: "nested",
      connector: "and",
      conditions: [
        {
          id: "nestedC1",
          leftOperand: { type: "hiddenField", value: "nf1" },
          operator: "equals",
          rightOperand: { type: "static", value: "nv1" },
        },
        deeplyNestedGroup,
      ],
    };

    const otherCondition: TSingleCondition = {
      id: "otherCondition",
      leftOperand: { type: "hiddenField", value: "other" },
      operator: "equals",
      rightOperand: { type: "static", value: "value" },
    };

    const group: TConditionGroup = {
      id: "parent",
      connector: "and",
      conditions: [nestedGroup, otherCondition],
    };

    // Remove the regular condition, leaving only the deeply nested group in the nested group
    const result = removeCondition(group, "nestedC1");
    expect(result).toBe(true);

    // The parent group should still have 2 conditions: the nested group and the other condition
    expect(group.conditions).toHaveLength(2);
    // The nested group should still be there but now contain only the deeply nested group
    expect(group.conditions[0]!.id).toBe("nested");
    expect((group.conditions[0] as TConditionGroup).conditions).toHaveLength(1);
    // The nested group should contain the flattened content from the deeply nested group
    expect((group.conditions[0] as TConditionGroup).conditions[0]!.id).toBe("deepC1");
    expect(group.conditions[1]!.id).toBe("otherCondition");
  });

  test("removeCondition removes empty groups after cleanup", () => {
    const emptyNestedGroup: TConditionGroup = {
      id: "emptyNested",
      connector: "and",
      conditions: [
        {
          id: "toBeRemoved",
          leftOperand: { type: "hiddenField", value: "f1" },
          operator: "equals",
          rightOperand: { type: "static", value: "v1" },
        },
      ],
    };

    const group: TConditionGroup = {
      id: "parent",
      connector: "and",
      conditions: [
        emptyNestedGroup,
        {
          id: "keepThis",
          leftOperand: { type: "hiddenField", value: "f2" },
          operator: "equals",
          rightOperand: { type: "static", value: "v2" },
        },
      ],
    };

    // Remove the only condition from the nested group
    const result = removeCondition(group, "toBeRemoved");
    expect(result).toBe(true);

    // The empty nested group should be removed, leaving only the other condition
    expect(group.conditions).toHaveLength(1);
    expect(group.conditions[0]!.id).toBe("keepThis");
  });

  test("deleteEmptyGroups with complex nested structure", () => {
    const deepEmptyGroup: TConditionGroup = { id: "deepEmpty", connector: "and", conditions: [] };
    const middleGroup: TConditionGroup = {
      id: "middle",
      connector: "or",
      conditions: [deepEmptyGroup],
    };
    const topGroup: TConditionGroup = {
      id: "top",
      connector: "and",
      conditions: [
        middleGroup,
        {
          id: "validCondition",
          leftOperand: { type: "hiddenField", value: "f" },
          operator: "equals",
          rightOperand: { type: "static", value: "v" },
        },
      ],
    };

    deleteEmptyGroups(topGroup);

    // Should remove the nested empty groups and keep only the valid condition
    expect(topGroup.conditions).toHaveLength(1);
    expect(topGroup.conditions[0]!.id).toBe("validCondition");
  });

  // Additional tests for complete coverage

  test("addConditionBelow with nested group correctly adds condition", () => {
    const nestedGroup: TConditionGroup = {
      id: "nestedGroup",
      connector: "and",
      conditions: [
        {
          id: "nestedC1",
          leftOperand: { type: "hiddenField", value: "nf1" },
          operator: "equals",
          rightOperand: { type: "static", value: "nv1" },
        },
      ],
    };

    const group: TConditionGroup = {
      id: "parentGroup",
      connector: "and",
      conditions: [nestedGroup],
    };

    const newCond: TSingleCondition = {
      id: "new",
      leftOperand: { type: "hiddenField", value: "x" },
      operator: "equals",
      rightOperand: { type: "static", value: "y" },
    };

    addConditionBelow(group, "nestedGroup", newCond);
    expect(group.conditions[1]).toEqual(newCond);

    addConditionBelow(group, "nestedC1", newCond);
    expect((group.conditions[0] as TConditionGroup).conditions[1]).toEqual(newCond);
  });

  test("missing connector in group defaults to 'and'", () => {
    const group: TConditionGroup = {
      id: "g1",
      conditions: [
        {
          id: "c1",
          leftOperand: { type: "hiddenField", value: "f1" },
          operator: "equals",
          rightOperand: { type: "static", value: "v1" },
        },
        {
          id: "c2",
          leftOperand: { type: "hiddenField", value: "f2" },
          operator: "equals",
          rightOperand: { type: "static", value: "v2" },
        },
      ],
    } as never; // Intentionally missing connector

    createGroupFromResource(group, "c1");
    expect(group.connector).toBe("and");
  });
});
