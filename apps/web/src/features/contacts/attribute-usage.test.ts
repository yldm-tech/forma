import type { TBaseFilter, TBaseFilters } from "@forma/types/segment";
import { describe, expect, test } from "vite-plus/test";
import { attributeKeyUsage } from "./attribute-usage";

// The warning shown before attribute keys are deleted: which segments and surveys filter on them.

const attribute = (id: string, key: string, connector: "and" | "or" | null = null): TBaseFilter => ({
  id,
  connector,
  resource: {
    id: `${id}-filter`,
    root: { type: "attribute", contactAttributeKey: key },
    qualifier: { operator: "equals" },
    value: "pro",
  },
});

const inSegment = (id: string, segmentId: string): TBaseFilter => ({
  id,
  connector: null,
  resource: {
    id: `${id}-filter`,
    root: { type: "segment", segmentId },
    qualifier: { operator: "userIsIn" },
    value: segmentId,
  },
});

const group = (id: string, filters: TBaseFilters): TBaseFilter => ({
  id,
  connector: "and",
  resource: filters,
});

const segment = (
  id: string,
  filters: TBaseFilters,
  options: { isPrivate?: boolean; active?: string[]; inactive?: string[] } = {}
) => ({
  id,
  title: options.isPrivate ? `private-${id}` : `Segment ${id}`,
  isPrivate: options.isPrivate ?? false,
  filters,
  activeSurveys: (options.active ?? []).map((name) => ({ id: `survey-${name}`, name })),
  inactiveSurveys: (options.inactive ?? []).map((name) => ({ id: `survey-${name}`, name })),
});

describe("attributeKeyUsage", () => {
  test("finds a key in a nested group, and lists the surveys of public and private segments", () => {
    const segments = [
      // "Pro & Enterprise": plan = pro or (country = jp and plan = enterprise), nested one level down.
      segment("pro", [attribute("a", "country"), group("g", [attribute("b", "plan", "or")])], {
        active: ["Checkout NPS"],
      }),
      // A survey's inline filter lives in its private segment.
      segment("inline", [attribute("c", "plan")], { isPrivate: true, inactive: ["Pricing Poll"] }),
      segment("japan", [attribute("d", "country")], { active: ["Japan survey"] }),
    ];
    expect(attributeKeyUsage(segments, ["plan"])).toEqual({
      segments: ["Segment pro"],
      surveys: ["Checkout NPS", "Pricing Poll"],
    });
  });

  test("a segment that filters on an affected segment is affected too, however deep the chain", () => {
    const segments = [
      segment("base", [attribute("a", "plan")]),
      segment("mid", [inSegment("b", "base")]),
      segment("top", [group("g", [inSegment("c", "mid")])], { active: ["Top survey"] }),
      segment("other", [inSegment("d", "unrelated")], { active: ["Other survey"] }),
    ];
    expect(attributeKeyUsage(segments, ["plan"])).toEqual({
      segments: ["Segment base", "Segment mid", "Segment top"],
      surveys: ["Top survey"],
    });
  });

  test("a survey targeted through two affected segments is listed once", () => {
    const segments = [
      segment("one", [attribute("a", "plan")], { active: ["Shared"] }),
      segment("two", [attribute("b", "role")], { inactive: ["Shared"] }),
    ];
    expect(attributeKeyUsage(segments, ["plan", "role"]).surveys).toEqual(["Shared"]);
  });

  test("nothing uses a key no filter names", () => {
    expect(attributeKeyUsage([segment("one", [attribute("a", "plan")])], ["country"])).toEqual({
      segments: [],
      surveys: [],
    });
    expect(attributeKeyUsage([], ["plan"])).toEqual({ segments: [], surveys: [] });
  });
});
