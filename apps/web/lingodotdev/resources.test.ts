import { describe, expect, test } from "vitest";
import { PUBLIC_I18N_NAMESPACES } from "./namespaces";
import { loadI18nResources } from "./resources";

describe("loadI18nResources", () => {
  test("returns only the namespaces asked for", async () => {
    const bundle = await loadI18nResources("en-US", ["common", "s"]);

    expect(Object.keys(bundle).sort()).toEqual(["common", "s"]);
    // The admin catalogue is the reason this function exists: 136 KB that must not cross to a
    // respondent's browser.
    expect(bundle.workspace).toBeUndefined();
  });

  test("the public set it is called with is actually present in the catalogue", async () => {
    // Catches a namespace renamed in `en-US.json` but not in `namespaces.ts`, which would silently
    // ship a route one slice short.
    const bundle = await loadI18nResources("en-US", PUBLIC_I18N_NAMESPACES);

    expect(Object.keys(bundle).sort()).toEqual([...PUBLIC_I18N_NAMESPACES].sort());
  });

  test("carries the real strings, not an empty shell", async () => {
    const bundle = await loadI18nResources("en-US", ["common"]);

    expect(typeof bundle.common).toBe("object");
    expect(Object.keys(bundle.common).length).toBeGreaterThan(50);
  });

  test("drops a namespace the catalogue does not have instead of throwing", async () => {
    const bundle = await loadI18nResources("en-US", ["common", "not_a_namespace"]);

    expect(Object.keys(bundle)).toEqual(["common"]);
  });

  test("falls back to the default catalogue for an unknown locale", async () => {
    const bundle = await loadI18nResources("xx-XX", ["common"]);

    // A locale with no catalogue must still render English, not an empty page.
    expect(Object.keys(bundle)).toEqual(["common"]);
  });

  test("loads a non-default shipped locale", async () => {
    const bundle = await loadI18nResources("ja-JP", ["common"]);

    expect(Object.keys(bundle)).toEqual(["common"]);
  });
});
