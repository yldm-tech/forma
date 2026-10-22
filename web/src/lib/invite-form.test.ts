import { expect, test } from "vite-plus/test";
import { validateInviteForm } from "./invite-form";

const form = { name: "Bob", email: "bob@work.com", role: "member" as const, teamIds: [] };

test("validateInviteForm", () => {
  expect(validateInviteForm(form, false)).toEqual({});
  expect(validateInviteForm({ ...form, name: " " }, false).name).toBe(
    "Name should be at least 1 character long"
  );
  expect(validateInviteForm({ ...form, name: "Bob <b>" }, false).name).toBe("Invalid name format");
  expect(validateInviteForm({ ...form, email: "" }, false).email).toBe("Email is required");
  expect(validateInviteForm({ ...form, email: "bob" }, false).email).toBe("Invalid email");
});

test("a team admin must pick at least one of their teams", () => {
  expect(validateInviteForm(form, true).teamIds).toBe("Team admins must select at least one team");
  expect(validateInviteForm({ ...form, teamIds: ["t1"] }, true)).toEqual({});
});
