import { expect, test } from "vite-plus/test";
import { validateSetupInvites } from "./setup-invites";

test("every row needs an address and a name; addresses are lowercased", () => {
  expect(validateSetupInvites([{ email: " Ada@Example.com ", name: " Ada " }])).toEqual({
    invitees: [{ email: "ada@example.com", name: "Ada" }],
    errors: [{}],
  });
  expect(validateSetupInvites([{ email: "", name: "" }]).errors).toEqual([
    { email: "Invalid email address", name: "Name should be at least 1 character long" },
  ]);
  expect(validateSetupInvites([{ email: "nope", name: "Ada" }]).errors).toEqual([
    { email: "Invalid email address" },
  ]);
});
