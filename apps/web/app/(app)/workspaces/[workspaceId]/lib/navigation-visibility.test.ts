import { describe, expect, test } from "vitest";
import { getVisibleNavigationSections } from "./navigation-visibility";

const section = (id: string, ...hidden: boolean[]) => ({
  id,
  items: hidden.map((isHidden, index) => ({ name: `${id}-${index}`, isHidden })),
});

describe("getVisibleNavigationSections", () => {
  test("drops a section whose every item is hidden", () => {
    const sections = [section("ask", false, true), section("act", true)];

    expect(getVisibleNavigationSections(sections).map((s) => s.id)).toEqual(["ask"]);
  });

  test("keeps a section that still has one visible item", () => {
    const sections = [section("ask", true, false)];

    expect(getVisibleNavigationSections(sections).map((s) => s.id)).toEqual(["ask"]);
  });

  test("keeps every section when nothing is hidden", () => {
    const sections = [section("ask", false, false), section("act", false)];

    expect(getVisibleNavigationSections(sections).map((s) => s.id)).toEqual(["ask", "act"]);
  });

  test("drops a section with no items at all", () => {
    expect(getVisibleNavigationSections([section("act")])).toEqual([]);
  });

  // Contacts carried no isHidden at all before this, so "unset" has to keep meaning "shown".
  test("treats an absent isHidden as visible", () => {
    const sections: { id: string; items: { name: string; isHidden?: boolean }[] }[] = [
      { id: "ask", items: [{ name: "surveys" }] },
    ];

    expect(getVisibleNavigationSections(sections).map((s) => s.id)).toEqual(["ask"]);
  });
});
