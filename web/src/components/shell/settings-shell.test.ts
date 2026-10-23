import { expect, test } from "vite-plus/test";
import { currentSettingsWorkspace } from "./settings-shell";

// forma2's getSettingsLayoutData: the last opened workspace while the user can still open it in this organization, else the first one they can open.

const workspaces = [
  { id: "w1", name: "Website" },
  { id: "w2", name: "App" },
];

test.each([
  ["the remembered workspace", "w2", "w2"],
  ["the first workspace when none is remembered", null, "w1"],
  ["the first workspace when the remembered one is elsewhere or gone", "w9", "w1"],
])("is %s", (_name, remembered, expected) => {
  expect(currentSettingsWorkspace(workspaces, remembered)?.id).toBe(expected);
});

test("is none for a user without a workspace in the organization", () => {
  expect(currentSettingsWorkspace([], "w1")).toBeUndefined();
  expect(currentSettingsWorkspace(undefined, "w1")).toBeUndefined();
});
