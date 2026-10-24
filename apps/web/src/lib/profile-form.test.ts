import { expect, test } from "vite-plus/test";
import { profileChanges } from "./profile-form";

const current = { name: "Ada", email: "ada@example.com", locale: "en-US" };

test("profileChanges separates the profile fields from an address change", () => {
  expect(profileChanges(current, { ...current })).toEqual({ profile: null, email: null });
  expect(profileChanges(current, { ...current, name: " Grace " })).toEqual({
    profile: { name: "Grace" },
    email: null,
  });
  expect(profileChanges(current, { ...current, locale: "ja-JP" })).toEqual({
    profile: { locale: "ja-JP" },
    email: null,
  });
  expect(profileChanges(current, { ...current, email: " ADA@example.com " })).toEqual({
    profile: null,
    email: null,
  });
  expect(profileChanges(current, { ...current, email: " New@Example.com" })).toEqual({
    profile: null,
    email: "new@example.com",
  });
});
