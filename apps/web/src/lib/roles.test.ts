import { beforeAll, describe, expect, test } from "vite-plus/test";
import type { i18n as I18n } from "i18next";
import { changeLocale, initI18n } from "./i18n";
import {
  type OrganizationRole,
  accessFlags,
  assignableRoles,
  canChangeSuspension,
  canDeleteRow,
  canManageTeam,
  defaultTeamRole,
  hasAnotherActiveOwner,
  isRoleEditDisabled,
  roleLabel,
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

test("hasAnotherActiveOwner counts the active, unsuspended owners other than the target", () => {
  const owner = (userId: string) => ({ userId, role: "owner" as const, isActive: true, suspendedAt: null });
  const suspended = { ...owner("s"), suspendedAt: "2026-10-26T00:00:00Z" };
  expect(hasAnotherActiveOwner([owner("a")], "a")).toBe(false);
  expect(hasAnotherActiveOwner([owner("a"), { ...owner("b"), isActive: false }], "a")).toBe(false);
  expect(hasAnotherActiveOwner([owner("a"), { ...owner("b"), role: "manager" }], "a")).toBe(false);
  expect(hasAnotherActiveOwner([owner("a"), suspended], "a")).toBe(false);
  expect(hasAnotherActiveOwner([owner("a"), owner("b")], "a")).toBe(true);
  // A suspended owner may be acted on while one active owner remains.
  expect(hasAnotherActiveOwner([owner("a"), suspended], "s")).toBe(true);
  expect(hasAnotherActiveOwner([owner("a")], undefined)).toBe(true);
});

describe("isRoleEditDisabled (forma2's role-edit-rules)", () => {
  const base = {
    isUserManagementDisabled: false,
    currentUserRole: "owner" as OrganizationRole,
    currentUserId: ME,
    memberRole: "member" as OrganizationRole,
    memberUserId: "other",
    hasAnotherActiveOwner: false,
  };
  test.each([
    ["an owner editing a member", {}, false],
    ["user management disabled", { isUserManagementDisabled: true }, true],
    ["your own role", { memberUserId: ME }, true],
    ["the last owner", { memberRole: "owner" }, true],
    ["one of several owners", { memberRole: "owner", hasAnotherActiveOwner: true }, false],
    [
      "a pending owner invite, which is not a membership",
      { memberRole: "owner", memberUserId: undefined },
      false,
    ],
    [
      "a manager editing an owner",
      { currentUserRole: "manager", memberRole: "owner", hasAnotherActiveOwner: true },
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
  const ctx = (currentUserRole: OrganizationRole, hasAnotherActiveOwner = false) => ({
    currentUserRole,
    currentUserId: ME,
    hasAnotherActiveOwner,
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

describe("roleLabel", () => {
  let i18n: I18n;
  beforeAll(async () => {
    i18n = await initI18n("en-US");
  });

  test("names every role in the user's language, never by its enum value", async () => {
    const roles: OrganizationRole[] = ["owner", "manager", "member", "billing"];
    expect(roles.map((role) => roleLabel(i18n.t, role))).toEqual(["Owner", "Manager", "Member", "Billing"]);
    await changeLocale(i18n, "ja-JP");
    for (const role of roles) {
      const label = roleLabel(i18n.t, role);
      expect(label).not.toBe(role);
      expect(label).not.toMatch(/^workspace\./);
    }
    await changeLocale(i18n, "zh-Hans-CN");
    expect(roleLabel(i18n.t, "owner")).not.toMatch(/[A-Za-z]/);
  });
});

describe("canChangeSuspension (the server's suspension rules)", () => {
  const member = (role: OrganizationRole, suspended = false, userId = "other") => ({
    userId,
    role,
    suspended,
  });
  const ctx = (currentUserRole: OrganizationRole, hasAnotherActiveOwner = false, canManageUsers = true) => ({
    currentUserRole,
    currentUserId: ME,
    hasAnotherActiveOwner,
    canManageUsers,
  });
  test.each([
    ["owner suspends a member", member("member"), ctx("owner"), true],
    ["manager suspends a manager", member("manager"), ctx("manager"), true],
    ["nobody suspends themselves", member("owner", false, ME), ctx("owner", true), false],
    ["manager cannot suspend an owner", member("owner"), ctx("manager", true), false],
    ["manager cannot restore an owner", member("owner", true), ctx("manager", true), false],
    ["owner cannot suspend the last active owner", member("owner"), ctx("owner", false), false],
    ["owner suspends another owner while one remains", member("owner"), ctx("owner", true), true],
    ["owner restores a suspended owner", member("owner", true), ctx("owner", false), true],
    ["no user management, no suspension", member("member"), ctx("manager", false, false), false],
    ["a member suspends nobody", member("member"), ctx("member"), false],
  ] as const)("%s", (_, row, context, expected) => {
    expect(canChangeSuspension(row, context)).toBe(expected);
  });
});
