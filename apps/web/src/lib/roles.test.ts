import { describe, expect, test } from "vite-plus/test";
import {
  type OrganizationRole,
  accessFlags,
  assignableRoles,
  canDeleteRow,
  canManageTeam,
  defaultTeamRole,
  hasMoreThanOneActiveOwner,
  isRoleEditDisabled,
} from "./roles";

const ME = "me";

describe("accessFlags", () => {
  test.each([
    ["owner", { isOwner: true, isOwnerOrManager: true, isBilling: false }],
    ["manager", { isManager: true, isOwnerOrManager: true }],
    ["member", { isMember: true, isOwnerOrManager: false }],
    ["billing", { isBilling: true, isOwnerOrManager: false }],
    [
      undefined,
      { isOwner: false, isManager: false, isMember: false, isBilling: false, isOwnerOrManager: false },
    ],
  ] as const)("%s", (role, expected) => {
    expect(accessFlags(role)).toMatchObject(expected);
  });
});

test("assignableRoles: managers assign members only; owners every role, billing only where allowed", () => {
  expect(assignableRoles("manager", true)).toEqual(["member"]);
  expect(assignableRoles("member", true)).toEqual(["member"]);
  expect(assignableRoles("owner", false)).toEqual(["owner", "manager", "member"]);
  expect(assignableRoles("owner", true)).toEqual(["billing", "owner", "manager", "member"]);
});

test("hasMoreThanOneActiveOwner counts active owner memberships only", () => {
  const owner = { role: "owner" as const, isActive: true };
  expect(hasMoreThanOneActiveOwner([owner])).toBe(false);
  expect(hasMoreThanOneActiveOwner([owner, { role: "owner", isActive: false }])).toBe(false);
  expect(hasMoreThanOneActiveOwner([owner, { role: "manager", isActive: true }])).toBe(false);
  expect(hasMoreThanOneActiveOwner([owner, owner])).toBe(true);
});

describe("isRoleEditDisabled (forma2's role-edit-rules)", () => {
  const base = {
    isUserManagementDisabled: false,
    currentUserRole: "owner" as OrganizationRole,
    currentUserId: ME,
    memberRole: "member" as OrganizationRole,
    memberUserId: "other",
    doesOrgHaveMoreThanOneOwner: false,
  };
  test.each([
    ["an owner editing a member", {}, false],
    ["user management disabled", { isUserManagementDisabled: true }, true],
    ["your own role", { memberUserId: ME }, true],
    ["the last owner", { memberRole: "owner" }, true],
    ["one of several owners", { memberRole: "owner", doesOrgHaveMoreThanOneOwner: true }, false],
    [
      "a pending owner invite, which is not a membership",
      { memberRole: "owner", memberUserId: undefined },
      false,
    ],
    [
      "a manager editing an owner",
      { currentUserRole: "manager", memberRole: "owner", doesOrgHaveMoreThanOneOwner: true },
      true,
    ],
    [
      "a manager editing an owner invite",
      { currentUserRole: "manager", memberRole: "owner", memberUserId: undefined },
      true,
    ],
    ["a manager editing a member", { currentUserRole: "manager" }, false],
  ] as const)("%s", (_, override, expected) => {
    expect(isRoleEditDisabled({ ...base, ...override })).toBe(expected);
  });
});

describe("canDeleteRow (forma2's showDeleteButton)", () => {
  const member = (role: OrganizationRole, userId = "other") => ({ kind: "member" as const, userId, role });
  const ctx = (currentUserRole: OrganizationRole, doesOrgHaveMoreThanOneOwner = false) => ({
    currentUserRole,
    currentUserId: ME,
    doesOrgHaveMoreThanOneOwner,
  });
  test.each([
    ["owner revokes an invite", { kind: "invite" } as const, ctx("owner"), true],
    ["manager revokes an invite", { kind: "invite" } as const, ctx("manager"), true],
    ["member cannot revoke an invite", { kind: "invite" } as const, ctx("member"), false],
    ["nobody removes themselves", member("owner", ME), ctx("owner", true), false],
    ["manager removes a member", member("member"), ctx("manager"), true],
    ["manager removes a manager", member("manager"), ctx("manager"), true],
    ["manager cannot remove an owner", member("owner"), ctx("manager", true), false],
    ["owner cannot remove the other owner when it is the last", member("owner"), ctx("owner", false), false],
    ["owner removes another owner while one remains", member("owner"), ctx("owner", true), true],
    ["billing removes nobody", member("member"), ctx("billing"), false],
  ] as const)("%s", (_, row, context, expected) => {
    expect(canDeleteRow(row, context)).toBe(expected);
  });
});

test("canManageTeam: owners and managers any team, others only teams they administer", () => {
  expect(canManageTeam({}, true)).toBe(true);
  expect(canManageTeam({ userRole: "admin" }, false)).toBe(true);
  expect(canManageTeam({ userRole: "contributor" }, false)).toBe(false);
  expect(canManageTeam({}, false)).toBe(false);
});

test("defaultTeamRole: owners and managers join teams as admins", () => {
  expect(defaultTeamRole("owner")).toBe("admin");
  expect(defaultTeamRole("manager")).toBe("admin");
  expect(defaultTeamRole("member")).toBe("contributor");
  expect(defaultTeamRole(undefined)).toBe("contributor");
});
