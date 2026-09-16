import { describe, expect, test } from "vitest";
import { OrganizationAccessType } from "@forma/types/api-key";
import {
  getOrganizationAuthorizationActionForAccessType,
  getWorkspaceAuthorizationAction,
  getWorkspaceAuthorizationActionForMethod,
} from "./permission-action";

describe("semantic authorization action mapping", () => {
  test.each([
    [undefined, "workspace.read"],
    ["read", "workspace.read"],
    ["readWrite", "workspace.write"],
    ["manage", "workspace.manage"],
  ] as const)("maps workspace permission %s", (permission, action) => {
    expect(getWorkspaceAuthorizationAction(permission)).toBe(action);
  });

  test.each([
    ["GET", "workspace.read"],
    ["POST", "workspace.write"],
    ["PUT", "workspace.write"],
    ["PATCH", "workspace.write"],
    ["DELETE", "workspace.manage"],
  ] as const)("maps HTTP %s", (method, action) => {
    expect(getWorkspaceAuthorizationActionForMethod(method)).toBe(action);
  });

  test.each([
    [OrganizationAccessType.Read, "organization.read_access"],
    [OrganizationAccessType.Write, "organization.manage_access"],
  ] as const)("maps organization access %s", (accessType, action) => {
    expect(getOrganizationAuthorizationActionForAccessType(accessType)).toBe(action);
  });
});
