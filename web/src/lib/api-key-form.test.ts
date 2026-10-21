import { expect, test } from "vite-plus/test";
import { canSubmitApiKey, effectiveAccess, hasDuplicateWorkspace } from "./api-key-form";

const none = { accessControl: { read: false, write: false } };

test("canSubmitApiKey needs a label that is not blank and at least one grant", () => {
  const row = { workspaceId: "w1", permission: "read" as const };
  expect(canSubmitApiKey("CI", [row], none)).toBe(true);
  expect(canSubmitApiKey("CI", [], { accessControl: { read: true, write: false } })).toBe(true);
  expect(canSubmitApiKey("CI", [], { accessControl: { read: false, write: true } })).toBe(true);
  expect(canSubmitApiKey("CI", [], none)).toBe(false);
  expect(canSubmitApiKey("   ", [row], none)).toBe(false);
});

test("hasDuplicateWorkspace", () => {
  expect(
    hasDuplicateWorkspace([
      { workspaceId: "w1", permission: "read" },
      { workspaceId: "w1", permission: "write" },
    ])
  ).toBe(true);
  expect(
    hasDuplicateWorkspace([
      { workspaceId: "w1", permission: "read" },
      { workspaceId: "w2", permission: "read" },
    ])
  ).toBe(false);
});

test("effectiveAccess: write implies read", () => {
  expect(effectiveAccess({ accessControl: { read: false, write: true } })).toEqual({
    accessControl: { read: true, write: true },
  });
  expect(effectiveAccess(none)).toEqual(none);
});
