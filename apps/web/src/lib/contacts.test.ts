import { expect, test } from "vite-plus/test";
import type { AttributeKey } from "./attribute-keys";
import {
  contactIdentifier,
  contactTableRows,
  defaultColumnVisibility,
  formatAttributeValue,
  hiddenColumnMatches,
  highlightParts,
} from "./contacts";

test("attribute values display by their type", () => {
  expect(formatAttributeValue("", "string", "en-US")).toBe("-");
  expect(formatAttributeValue(null, "number", "en-US")).toBe("-");
  expect(formatAttributeValue("1234.5", "number", "en-US")).toBe("1,234.5");
  expect(formatAttributeValue("abc", "number", "en-US")).toBe("abc");
  expect(formatAttributeValue("2026-10-22T12:00:00.000Z", "date", "en-US")).toBe("Oct 22, 2026");
  expect(formatAttributeValue("not a date", "date", "en-US")).toBe("not a date");
});

test("a contact is named by its email, then its userId", () => {
  expect(contactIdentifier({ email: "a@x.com", userId: "1" })).toBe("a@x.com");
  expect(contactIdentifier({ email: "", userId: "1" })).toBe("1");
  expect(contactIdentifier({})).toBe("");
});

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

test("table rows lift the default keys and list every other key", () => {
  const rows = contactTableRows(
    [{ id: "c1", attributes: { email: "a@x.com", plan: "pro" } }],
    [key("email"), key("userId"), key("plan"), key("age", "number")]
  );
  expect(rows).toEqual([
    {
      id: "c1",
      userId: "",
      email: "a@x.com",
      firstName: "",
      lastName: "",
      attributes: [
        { key: "plan", name: null, value: "pro", dataType: "string" },
        { key: "age", name: null, value: "", dataType: "number" },
      ],
    },
  ]);
  expect(defaultColumnVisibility(["select", "contactsTableUser", "userId", "email", "plan"], rows)).toEqual({
    select: true,
    contactsTableUser: false,
    userId: false,
    email: true,
    firstName: true,
    lastName: true,
    plan: false,
  });
});

test("search matches are highlighted case-insensitively and literally", () => {
  expect(highlightParts("Alice@Example.com", "example")).toEqual([
    { text: "Alice@", match: false },
    { text: "Example", match: true },
    { text: ".com", match: false },
  ]);
  expect(highlightParts("a.b", ".")).toEqual([
    { text: "a", match: false },
    { text: ".", match: true },
    { text: "b", match: false },
  ]);
  expect(highlightParts("abc", " ")).toEqual([{ text: "abc", match: false }]);
});

test("a search hit in a hidden column is found, as the server matches it", () => {
  const cells = [
    { id: "email", label: "Email", value: "akira@example.com" },
    { id: "plan", label: "Plan", value: "Pro" },
    { id: "country", label: "Country", value: "Japan" },
  ];
  const visibility = { email: true, plan: false, country: false };
  // Case-insensitive and trimmed; a hit in a visible column needs no explanation.
  expect(hiddenColumnMatches(cells, " pro ", visibility)).toEqual([cells[1]]);
  expect(hiddenColumnMatches(cells, "akira", visibility)).toEqual([]);
  expect(hiddenColumnMatches(cells, "a", visibility)).toEqual([cells[2]]);
  expect(hiddenColumnMatches(cells, "  ", visibility)).toEqual([]);
  // A column without a stored choice counts as shown.
  expect(hiddenColumnMatches(cells, "japan", { plan: false })).toEqual([]);
});
