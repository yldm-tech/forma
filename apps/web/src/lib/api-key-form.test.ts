import { expect, test } from "vite-plus/test";
import { apiKeyScope, canSubmitApiKey, effectiveAccess, hasDuplicateWorkspace } from "./api-key-form";

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

test("apiKeyScope lists the first workspace grants, counts the rest and reads write as including read", () => {
  const grant = (name: string) => ({ workspaceId: name, workspaceName: name, permission: "read" as const });
  expect(
    apiKeyScope({ workspacePermissions: [grant("A"), grant("B"), grant("C")], organizationAccess: none })
  ).toEqual({ shown: [grant("A"), grant("B")], more: 1, organization: null });
  expect(
    apiKeyScope({
      workspacePermissions: [],
      organizationAccess: { accessControl: { read: false, write: true } },
    })
  ).toEqual({ shown: [], more: 0, organization: "write" });
  expect(
    apiKeyScope({
      workspacePermissions: [grant("A")],
      organizationAccess: { accessControl: { read: true, write: false } },
    })
  ).toEqual({ shown: [grant("A")], more: 0, organization: "read" });
});
