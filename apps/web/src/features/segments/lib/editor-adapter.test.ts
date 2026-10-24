import type { TBaseFilters, TSegment } from "@forma/types/segment";
import { describe, expect, test, vi } from "vite-plus/test";
import type { AttributeKey } from "../../../lib/attribute-keys";
import type { Segment } from "../../../lib/queries/contacts";
import { formSegment, formSetSegment, toContactAttributeKey, toEditorSegment } from "./editor-adapter";

const tree: TBaseFilters = [
  {
    id: "n1",
    connector: null,
    resource: {
      id: "f1",
      root: { type: "device", deviceType: "phone" },
      qualifier: { operator: "equals" },
      value: "phone",
    },
  },
];

describe("formSegment", () => {
  test("carries the form's tree, workspace and the surveys the segment gates", () => {
    const segment = formSegment({
      segmentId: "s1",
      workspaceId: "ws",
      filters: tree,
      linkedSurveyIds: ["v1"],
    });
    expect(segment).toMatchObject({ id: "s1", workspaceId: "ws", filters: tree, surveys: ["v1"] });
  });

  test("a new segment has no id", () => {
    expect(formSegment({ workspaceId: "ws", filters: [], linkedSurveyIds: [] }).id).toBe("");
  });
});

describe("formSetSegment", () => {
  const current = formSegment({ workspaceId: "ws", filters: [], linkedSurveyIds: [] });

  test("keeps the tree of the segment the editor sets", () => {
    const setFilters = vi.fn();
    formSetSegment(current, setFilters)({ ...current, filters: tree });
    expect(setFilters).toHaveBeenCalledWith(tree);
  });

  test("applies an updater to the current segment", () => {
    const setFilters = vi.fn();
    formSetSegment(current, setFilters)((segment: TSegment | null) => ({ ...segment!, filters: tree }));
    expect(setFilters).toHaveBeenCalledWith(tree);
  });

  test("ignores a null, which would leave the form without a tree", () => {
    const setFilters = vi.fn();
    formSetSegment(current, setFilters)(null);
    expect(setFilters).not.toHaveBeenCalled();
  });
});

test("an attribute key and a listed segment get the dates the editor's types carry", () => {
  const key: AttributeKey = {
    id: "k1",
    createdAt: "2026-01-02T03:04:05.000Z",
    updatedAt: "2026-02-03T04:05:06.000Z",
    key: "plan",
    name: "Plan",
    description: null,
    isUnique: false,
    type: "custom",
    dataType: "string",
    workspaceId: "ws",
  };
  expect(toContactAttributeKey(key)).toEqual({
    ...key,
    createdAt: new Date("2026-01-02T03:04:05.000Z"),
    updatedAt: new Date("2026-02-03T04:05:06.000Z"),
  });

  const segment: Segment = {
    id: "s1",
    createdAt: "2026-01-02T03:04:05.000Z",
    updatedAt: "2026-02-03T04:05:06.000Z",
    title: "Pro",
    description: null,
    isPrivate: false,
    filters: tree,
    workspaceId: "ws",
    surveys: ["v1"],
    activeSurveys: [],
    inactiveSurveys: [],
  };
  expect(toEditorSegment(segment)).toEqual({
    id: "s1",
    title: "Pro",
    description: null,
    isPrivate: false,
    filters: tree,
    workspaceId: "ws",
    createdAt: new Date("2026-01-02T03:04:05.000Z"),
    updatedAt: new Date("2026-02-03T04:05:06.000Z"),
    surveys: ["v1"],
  });
});
