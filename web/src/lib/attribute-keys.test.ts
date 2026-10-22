import { expect, test } from "vite-plus/test";
import {
  type AttributeKey,
  filterAttributeKeys,
  isReservedAttributeKey,
  isStringMatch,
  newAttributeKeyError,
} from "./attribute-keys";
import { initI18n } from "./i18n";

const { t } = await initI18n("en-US");

const key = (k: string, name: string | null, description: string | null): AttributeKey => ({
  id: k,
  key: k,
  name,
  description,
  isUnique: false,
  type: "custom",
  dataType: "string",
  workspaceId: "w",
  createdAt: "",
  updatedAt: "",
});

test("isStringMatch ignores case, spaces, underscores and dashes", () => {
  expect(isStringMatch("first_name", "First Name")).toBe(true);
  expect(isStringMatch("Name", "First Name")).toBe(true);
  expect(isStringMatch("full name", "First Name")).toBe(false);
});

test("the reserved keys are refused whatever their case and padding", () => {
  expect(isReservedAttributeKey(" User_ID ")).toBe(true);
  expect(isReservedAttributeKey("userid")).toBe(false);
});

test("the attributes search looks at the key, the name and the description", () => {
  const keys = [key("plan", "Plan", null), key("age", null, "Years since birth")];
  expect(filterAttributeKeys(keys, "PLA").map((k) => k.key)).toEqual(["plan"]);
  expect(filterAttributeKeys(keys, "birth").map((k) => k.key)).toEqual(["age"]);
  expect(filterAttributeKeys(keys, "")).toHaveLength(2);
});

test("a new key is required, a safe identifier and not reserved", () => {
  expect(newAttributeKeyError("", t)).toBe(t("workspace.contacts.attribute_key_required"));
  expect(newAttributeKeyError("Plan", t)).toBe(
    t("workspace.contacts.attribute_key_safe_identifier_required")
  );
  expect(newAttributeKeyError("first_name", t)).toContain("user_id, first_name, last_name");
  expect(newAttributeKeyError("plan", t)).toBe("");
});
