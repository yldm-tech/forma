import { describe, expect, test } from "vite-plus/test";
import type { AttributeKey } from "./attribute-keys";
import {
  EXAMPLE_CSV,
  attributeMapErrors,
  csvRecordsError,
  duplicateMappedColumns,
  isCsvFile,
  isNewAttributeTarget,
  mappableColumns,
  parseContactsCsv,
  sniffDelimiter,
  suggestAttributeMap,
  typeMismatchMessage,
} from "./contacts-csv";
import { initI18n } from "./i18n";

const { t } = await initI18n("en-US");

const key = (
  id: string,
  keyName: string,
  name: string | null,
  type: "default" | "custom" = "default"
): AttributeKey => ({
  id,
  key: keyName,
  name,
  type,
  description: null,
  isUnique: false,
  dataType: "string",
  workspaceId: "w",
  createdAt: "",
  updatedAt: "",
});

const KEYS = [
  key("k-user", "userId", "User Id"),
  key("k-email", "email", "Email"),
  key("k-first", "firstName", "First Name"),
  key("k-last", "lastName", "Last Name"),
  key("k-plan", "plan", null, "custom"),
];

describe("parseContactsCsv", () => {
  test("reads the header and rows, trimming column names and skipping a BOM and empty lines", () => {
    const parsed = parseContactsCsv("﻿ email , plan\na@x.com,free\n\nb@x.com,pro\n");
    expect(parsed).toEqual({
      columns: ["email", "plan"],
      records: [
        { email: "a@x.com", plan: "free" },
        { email: "b@x.com", plan: "pro" },
      ],
    });
  });

  test("uses the delimiter the header line uses most", () => {
    expect(parseContactsCsv("email;plan\na@x.com;free").records).toEqual([
      { email: "a@x.com", plan: "free" },
    ]);
    expect(parseContactsCsv("email\tplan\na@x.com\tfree").records).toEqual([
      { email: "a@x.com", plan: "free" },
    ]);
    expect(parseContactsCsv('"a,b";c;d\n1;2;3').columns).toEqual(["a,b", "c", "d"]);
  });

  test("refuses rows with a different number of fields", () => {
    expect(() => parseContactsCsv("email,plan\na@x.com")).toThrow(/Invalid Record Length/);
  });

  test("an empty file has no columns", () => {
    expect(parseContactsCsv("")).toEqual({ columns: [], records: [] });
  });
});

test("sniffDelimiter counts only outside quotes and prefers the comma on a tie", () => {
  expect(sniffDelimiter('"x;y;z",a,b')).toBe(",");
  expect(sniffDelimiter("a;b,c")).toBe(",");
  expect(sniffDelimiter("a;b;c,d")).toBe(";");
});

test("only CSV files are taken", () => {
  expect(isCsvFile({ name: "contacts.csv", type: "" })).toBe(true);
  expect(isCsvFile({ name: "contacts.txt", type: "text/csv" })).toBe(true);
  expect(isCsvFile({ name: "contacts.csv", type: "application/pdf" })).toBe(false);
  expect(isCsvFile({ name: "contacts.txt", type: "" })).toBe(false);
});

describe("csvRecordsError", () => {
  test("every record needs a non-empty email", () => {
    expect(csvRecordsError([{ Email: "a@x.com" }])).toBe("Missing email field for one or more records");
    expect(csvRecordsError([{ email: "" }])).toBe("Email field is empty for one or more records");
  });

  test("emails and non-empty userIds are unique", () => {
    expect(csvRecordsError([{ email: "a" }, { email: "a" }])).toBe("Duplicate emails found in the records");
    expect(
      csvRecordsError([
        { email: "a", userId: "1" },
        { email: "b", userId: "1" },
      ])
    ).toBe("Duplicate userIds found in the records");
    expect(
      csvRecordsError([
        { email: "a", userId: "" },
        { email: "b", userId: "" },
      ])
    ).toBeNull();
  });

  test("at most 10000 records", () => {
    const records = Array.from({ length: 10001 }, (_, i) => ({ email: `${i}@x.com` }));
    expect(csvRecordsError(records)).toBe("Maximum 10000 records allowed at a time.");
  });
});

describe("mapping", () => {
  test("a column maps to the first key whose label contains it, ignoring case, spaces, _ and -", () => {
    expect(suggestAttributeMap(["E-mail", "first_name", "user id", "plan", "age"], KEYS)).toEqual({
      "E-mail": "k-email",
      first_name: "k-first",
      "user id": "k-user",
      plan: "k-plan",
      age: "age",
    });
  });

  test("only columns that match a key or can name a new one are listed", () => {
    expect(mappableColumns(["email", "Signup Date", "signup_date", "2fa"], KEYS)).toEqual([
      "email",
      "signup_date",
    ]);
  });

  test("new keys must be safe identifiers and not reserved", () => {
    const errors = attributeMapErrors(
      ["Signup Date", "user_id", "age", "email"],
      { "Signup Date": "Signup Date", user_id: "user_id", age: "age", email: "k-email" },
      KEYS,
      t
    );
    expect(errors).toHaveLength(2);
    expect(errors[0]).toContain("Signup Date");
    expect(errors[1]).toContain("user_id");
  });

  test("two columns may not fill one key", () => {
    expect(duplicateMappedColumns({ mail: "k-email", email: "k-email", plan: "k-plan" })).toEqual([
      "mail",
      "email",
    ]);
    expect(duplicateMappedColumns({ email: "k-email", plan: "k-plan" })).toEqual([]);
  });

  test("a target is new when it names no key and could become one", () => {
    expect(isNewAttributeTarget("age", KEYS)).toBe(true);
    expect(isNewAttributeTarget("k-email", KEYS)).toBe(false);
    expect(isNewAttributeTarget("Signup Date", KEYS)).toBe(false);
    expect(isNewAttributeTarget(undefined, KEYS)).toBe(false);
  });

  test("forma2's example file maps its reserved columns onto the default keys", () => {
    const { columns, records } = parseContactsCsv(EXAMPLE_CSV);
    expect(records).toHaveLength(5);
    expect(csvRecordsError(records)).toBeNull();
    const map = suggestAttributeMap(columns, KEYS);
    expect(map).toMatchObject({ user_id: "k-user", first_name: "k-first", last_name: "k-last" });
    expect(attributeMapErrors(columns, map, KEYS, t)).toEqual([]);
  });
});

test("a type mismatch shows three sample values and how many more", () => {
  expect(
    typeMismatchMessage({ key: "age", dataType: "number", invalidValues: ["a", "b", "c", "d", "e"] }, t)
  ).toContain("a, b, c (2 more)");
  expect(typeMismatchMessage({ key: "age", dataType: "number", invalidValues: ["a"] }, t)).not.toContain(
    "more"
  );
});
