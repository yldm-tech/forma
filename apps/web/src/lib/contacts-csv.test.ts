import { describe, expect, test } from "vite-plus/test";
import type { AttributeKey } from "./attribute-keys";
import {
  EXAMPLE_CSV,
  attributeMapErrors,
  csvRecordsError,
  csvRecordsErrorMessage,
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
    expect(csvRecordsError([{ Email: "a@x.com" }])).toEqual({
      code: "missing_email_column",
      columns: ["Email"],
    });
    // The empty email is reported on its spreadsheet row, the header being row 1.
    expect(csvRecordsError([{ email: "a@x.com" }, { email: "" }])).toEqual({ code: "empty_email", row: 3 });
  });

  test('a missing email column lists the columns that were found, so "mail" shows it is not "email"', () => {
    const { columns, records } = parseContactsCsv("name;mail\nAda;ada@example.com\n");
    expect(columns).toEqual(["name", "mail"]);
    expect(csvRecordsError(records, columns)).toEqual({
      code: "missing_email_column",
      columns: ["name", "mail"],
    });
    expect(csvRecordsErrorMessage({ code: "missing_email_column", columns }, t)).toContain("name, mail");
  });

  test("emails and non-empty userIds are unique", () => {
    expect(csvRecordsError([{ email: "a" }, { email: "a" }])).toEqual({
      code: "duplicate_email",
      email: "a",
    });
    expect(
      csvRecordsError([
        { email: "a", userId: "1" },
        { email: "b", userId: "1" },
      ])
    ).toEqual({ code: "duplicate_user_id", userId: "1" });
    expect(
      csvRecordsError([
        { email: "a", userId: "" },
        { email: "b", userId: "" },
      ])
    ).toBeNull();
  });

  test("at most 10000 records", () => {
    const records = Array.from({ length: 10001 }, (_, i) => ({ email: `${i}@x.com` }));
    expect(csvRecordsError(records)).toEqual({ code: "too_many_records", max: 10000 });
  });

  test("every problem has a translated message that names its values", () => {
    expect(csvRecordsErrorMessage({ code: "empty_email", row: 7 }, t)).toContain("7");
    expect(csvRecordsErrorMessage({ code: "duplicate_email", email: "a@x.com" }, t)).toContain("a@x.com");
    expect(csvRecordsErrorMessage({ code: "duplicate_user_id", userId: "u-1" }, t)).toContain("u-1");
    expect(csvRecordsErrorMessage({ code: "too_many_records", max: 10000 }, t)).toMatch(/10,?000/);
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
