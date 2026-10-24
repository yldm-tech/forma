import { expect, test } from "vite-plus/test";
import type { AttributeKey } from "./attribute-keys";
import {
  attributeRowsErrors,
  attributeUpdateMessage,
  attributesPayload,
  dayToStoredValue,
  storedDayForInput,
} from "./contact-attributes-form";
import { initI18n } from "./i18n";

const { t } = await initI18n("en-US");

const key = (k: string, dataType: AttributeKey["dataType"] = "string"): AttributeKey => ({
  id: k,
  key: k,
  name: null,
  description: null,
  isUnique: false,
  type: "custom",
  dataType,
  workspaceId: "w",
  createdAt: "",
  updatedAt: "",
});
const KEYS = [key("email"), key("userId"), key("age", "number"), key("joined", "date")];

test("every row needs a key and keys are unique", () => {
  const { rows } = attributeRowsErrors(
    [
      { key: "email", value: "a@x.com" },
      { key: "", value: "x" },
      { key: "age", value: "1" },
      { key: "age", value: "2" },
    ],
    KEYS,
    t
  );
  expect(rows).toEqual({
    1: { key: "Key is required" },
    2: { key: "Duplicate key: age" },
    3: { key: "Duplicate key: age" },
  });
});

test("an email or a userId is required, and an email must look like one", () => {
  expect(
    attributeRowsErrors(
      [
        { key: "email", value: " " },
        { key: "userId", value: "" },
      ],
      KEYS,
      t
    ).rows
  ).toEqual({
    0: { value: "Either email or userId is required" },
    1: { value: "Either email or userId is required" },
  });
  expect(attributeRowsErrors([{ key: "email", value: "nope" }], KEYS, t).rows).toEqual({
    0: { value: "Invalid email format" },
  });
  expect(attributeRowsErrors([], KEYS, t).form).toBe("At least one attribute is required");
});

test("number and date keys need a parseable value", () => {
  const { rows } = attributeRowsErrors(
    [
      { key: "userId", value: "u1" },
      { key: "age", value: "" },
      { key: "joined", value: "soon" },
    ],
    KEYS,
    t
  );
  expect(Object.keys(rows)).toEqual(["1", "2"]);
});

test("number values are sent as numbers", () => {
  expect(
    attributesPayload(
      [
        { key: "age", value: "42" },
        { key: "email", value: "a@x.com" },
        { key: "age2", value: "" },
      ],
      [...KEYS, key("age2", "number")]
    )
  ).toEqual({ age: 42, email: "a@x.com", age2: "" });
});

test("dates round-trip through the day picker as midnight UTC", () => {
  expect(storedDayForInput("2026-08-05T00:00:00.000Z")).toBe("2026-08-05");
  expect(storedDayForInput("2026-13-45")).toBe("");
  expect(storedDayForInput("")).toBe("");
  expect(dayToStoredValue("2026-08-05")).toBe("2026-08-05T00:00:00.000Z");
});

test("update messages are translated by code", () => {
  expect(attributeUpdateMessage("email_already_exists", {}, t)).not.toBe("email_already_exists");
  expect(attributeUpdateMessage("something_new", {}, t)).toBe("something_new");
});
