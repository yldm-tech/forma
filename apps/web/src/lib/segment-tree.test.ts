import type { TBaseFilters, TSegmentFilter } from "@forma/types/segment";
import { describe, expect, test } from "vite-plus/test";
import {
  appendFilter,
  canSaveSegment,
  changeAttributeKey,
  deleteResource,
  doesSegmentReferenceSegment,
  newSegmentFilter,
  operatorsForDataType,
  segmentActivitySummary,
} from "./segment-filters";

// Editing a segment's filter tree the way the segment editor does it: new filters with forma2's defaults, appended, re-keyed and removed, and the save rule over the result.

const resourceOf = (filters: TBaseFilters, index: number) => filters[index]?.resource as TSegmentFilter;

describe("newSegmentFilter", () => {
  test("gives each kind of filter forma2's defaults, joined with and", () => {
    const text = newSegmentFilter({
      type: "attribute",
      contactAttributeKey: "plan",
      attributeDataType: "string",
    });
    expect(text).toMatchObject({
      connector: "and",
      resource: {
        root: { type: "attribute", contactAttributeKey: "plan" },
        qualifier: { operator: "equals" },
        value: "",
      },
    });
    expect(
      newSegmentFilter({ type: "attribute", contactAttributeKey: "signup", attributeDataType: "date" })
    ).toMatchObject({
      resource: { qualifier: { operator: "isOlderThan" }, value: { amount: 1, unit: "days" } },
    });
    expect(newSegmentFilter({ type: "person" })).toMatchObject({
      resource: {
        root: { type: "person", personIdentifier: "userId" },
        qualifier: { operator: "equals" },
        value: "",
      },
    });
    expect(newSegmentFilter({ type: "segment", segmentId: "s1" })).toMatchObject({
      resource: {
        root: { type: "segment", segmentId: "s1" },
        qualifier: { operator: "userIsIn" },
        value: "s1",
      },
    });
    expect(newSegmentFilter({ type: "device", deviceType: "phone" })).toMatchObject({
      resource: {
        root: { type: "device", deviceType: "phone" },
        qualifier: { operator: "equals" },
        value: "phone",
      },
    });
    expect(newSegmentFilter({ type: "surveyInteraction" })).toMatchObject({
      resource: {
        qualifier: { operator: "haveSeen" },
        value: { surveyScope: "any", surveyIds: [], within: { amount: 1, unit: "months" } },
      },
    });
  });

  test("needs the key, segment or device its kind is about", () => {
    expect(newSegmentFilter({ type: "attribute" })).toBeNull();
    expect(newSegmentFilter({ type: "segment" })).toBeNull();
    expect(newSegmentFilter({ type: "device" })).toBeNull();
  });

  test("uses fresh cuid2 ids for the node and the filter", () => {
    const filter = newSegmentFilter({ type: "person" });
    expect(filter?.id).toMatch(/^[0-9a-z]+$/);
    expect(resourceOf([filter!], 0).id).toMatch(/^[0-9a-z]+$/);
    expect(filter?.id).not.toBe(resourceOf([filter!], 0).id);
  });
});

test("the first filter of a tree has no connector, later ones keep theirs", () => {
  const first = newSegmentFilter({ type: "person" })!;
  const second = newSegmentFilter({ type: "device", deviceType: "desktop" })!;
  const tree = appendFilter(appendFilter([], first), second);
  expect(tree.map((node) => node.connector)).toEqual([null, "and"]);
});

test("changing an attribute filter's key restarts its operator and value for the new type", () => {
  const tree = appendFilter(
    [],
    newSegmentFilter({ type: "attribute", contactAttributeKey: "plan", attributeDataType: "string" })!
  );
  const id = resourceOf(tree, 0).id;
  changeAttributeKey(tree, id, "signup", "date");
  expect(resourceOf(tree, 0)).toMatchObject({
    root: { contactAttributeKey: "signup" },
    qualifier: { operator: "isOlderThan" },
    value: { amount: 1, unit: "days" },
  });
  changeAttributeKey(tree, id, "age", "number");
  expect(resourceOf(tree, 0)).toMatchObject({ qualifier: { operator: "equals" }, value: "" });
});

test("operators follow the key's data type", () => {
  expect(operatorsForDataType("date")).toContain("isBetween");
  expect(operatorsForDataType("number")).toContain("greaterThan");
  expect(operatorsForDataType("string")).toContain("contains");
  expect(operatorsForDataType("string")).not.toContain("greaterThan");
});

describe("canSaveSegment", () => {
  test("needs a title and at least one valid filter", () => {
    const valid = appendFilter([], newSegmentFilter({ type: "device", deviceType: "phone" })!);
    expect(canSaveSegment("Power users", valid)).toBe(true);
    expect(canSaveSegment("  ", valid)).toBe(false);
    expect(canSaveSegment("Power users", [])).toBe(false);
  });

  test("refuses a tree the schema refuses, such as an empty value or a missing survey", () => {
    const emptyValue = appendFilter([], newSegmentFilter({ type: "person" })!);
    expect(canSaveSegment("x", emptyValue)).toBe(false);
    const interaction = appendFilter([], newSegmentFilter({ type: "surveyInteraction" })!);
    expect(canSaveSegment("x", interaction)).toBe(true);
    (resourceOf(interaction, 0).value as { surveyScope: string }).surveyScope = "specific";
    expect(canSaveSegment("x", interaction)).toBe(false);
  });

  test("a deleted filter leaves the next one first, without a connector", () => {
    const a = newSegmentFilter({ type: "device", deviceType: "phone" })!;
    const b = newSegmentFilter({ type: "device", deviceType: "desktop" })!;
    const tree = appendFilter(appendFilter([], a), b);
    deleteResource(tree, (a.resource as TSegmentFilter).id);
    expect(tree).toHaveLength(1);
    expect(tree[0]?.connector).toBeNull();
    expect(canSaveSegment("x", tree)).toBe(true);
  });
});

describe("segment activity", () => {
  const usesTarget: TBaseFilters = [
    {
      id: "g",
      connector: null,
      resource: [
        {
          id: "n",
          connector: null,
          resource: {
            id: "f",
            root: { type: "segment", segmentId: "target" },
            qualifier: { operator: "userIsNotIn" },
            value: "target",
          },
        },
      ],
    },
  ];

  test("a nested segment filter references its segment", () => {
    expect(doesSegmentReferenceSegment(usesTarget, "target")).toBe(true);
    expect(doesSegmentReferenceSegment(usesTarget, "other")).toBe(false);
  });

  test("a segment reaches its own surveys and those of segments using it, once each", () => {
    const target = {
      id: "target",
      filters: [],
      activeSurveys: [{ id: "s1", name: "Direct" }],
      inactiveSurveys: [],
    };
    const user = {
      id: "user",
      filters: usesTarget,
      activeSurveys: [
        { id: "s1", name: "Direct" },
        { id: "s2", name: "Indirect" },
      ],
      inactiveSurveys: [{ id: "s3", name: "Paused" }],
    };
    const unrelated = {
      id: "other",
      filters: [],
      activeSurveys: [{ id: "s4", name: "Elsewhere" }],
      inactiveSurveys: [],
    };
    expect(segmentActivitySummary(target, [target, user, unrelated])).toEqual({
      activeSurveys: ["Direct", "Indirect"],
      inactiveSurveys: ["Paused"],
    });
  });
});
