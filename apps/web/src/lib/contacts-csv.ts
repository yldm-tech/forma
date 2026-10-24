import { isSafeIdentifier } from "@forma/types/safe-identifier";
import { parse } from "csv-parse/browser/esm/sync";
import type { TFunction } from "i18next";
import {
  type AttributeKey,
  RESERVED_ATTRIBUTE_KEYS_TEXT,
  attributeKeyLabel,
  isReservedAttributeKey,
  isStringMatch,
} from "./attribute-keys";

// The contacts CSV import as forma2's upload dialog ran it (modules/contacts/components/upload-contacts-button.tsx, types/contact.ts), reading the file the way the Go server reads it again on upload (apps/server/internal/contact/csvimport.go).

/** The largest file the import takes, forma2's 800 KB. */
export const MAX_CSV_BYTES = 800 * 1024;

/** The most rows one import takes. */
export const MAX_CSV_RECORDS = 10000;

export type DuplicateContactsAction = "skip" | "update" | "overwrite";

/** A parsed file: its trimmed column names and its rows keyed by them. */
export interface CsvContacts {
  columns: string[];
  records: Record<string, string>[];
}

/** Whether a picked file may be imported: a CSV by name or declared type. */
export const isCsvFile = (file: { name: string; type: string }): boolean =>
  file.type ? file.type === "text/csv" || file.type.includes("csv") : file.name.endsWith(".csv");

/** The delimiter of a header line among comma, semicolon and tab, the one used most outside quotes (comma on a tie), as the server picks it. */
export const sniffDelimiter = (headerLine: string): "," | ";" | "\t" => {
  const counts = { ",": 0, ";": 0, "\t": 0 };
  let quoted = false;
  for (const char of headerLine) {
    if (char === '"') quoted = !quoted;
    else if (!quoted && (char === "," || char === ";" || char === "\t")) counts[char]++;
  }
  let best: "," | ";" | "\t" = ",";
  for (const delimiter of [";", "\t"] as const) if (counts[delimiter] > counts[best]) best = delimiter;
  return best;
};

/** Parses a contacts file: a byte order mark is skipped, the first row names the columns, empty lines are skipped, and every row must have as many fields as the header (csv-parse throws otherwise). */
export const parseContactsCsv = (text: string): CsvContacts => {
  const withoutBom = text.startsWith("﻿") ? text.slice(1) : text;
  const headerLine = withoutBom.split(/\r?\n|\r/, 1)[0] ?? "";
  const rows = parse(withoutBom, {
    bom: true,
    skip_empty_lines: true,
    delimiter: sniffDelimiter(headerLine),
  }) as string[][];
  const [header, ...body] = rows;
  if (!header) return { columns: [], records: [] };
  const columns = header.map((column) => column.trim());
  const records = body.map((row) => Object.fromEntries(columns.map((column, i) => [column, row[i] ?? ""])));
  return { columns, records };
};

/** A problem with the file's rows, as a code and the values its message names. */
export type CsvRecordsError =
  | { code: "too_many_records"; max: number }
  | { code: "missing_email_column"; columns: string[] }
  | { code: "empty_email"; row: number }
  | { code: "duplicate_email"; email: string }
  | { code: "duplicate_user_id"; userId: string };

/** The first value that appears twice, or undefined. */
const firstDuplicate = (values: string[]): string | undefined => {
  const seen = new Set<string>();
  for (const value of values) {
    if (seen.has(value)) return value;
    seen.add(value);
  }
  return undefined;
};

/**
 * forma2's ZContactCSVUploadResponse on the file as read: the first problem, or null. An empty email names its row as a spreadsheet numbers it (the header is row 1), and a missing email column lists the columns that were found, which shows a file split on the wrong delimiter.
 */
export const csvRecordsError = (
  records: Record<string, string>[],
  columns: string[] = Object.keys(records[0] ?? {})
): CsvRecordsError | null => {
  if (records.length > MAX_CSV_RECORDS) return { code: "too_many_records", max: MAX_CSV_RECORDS };
  for (const [index, record] of records.entries()) {
    if (!Object.hasOwn(record, "email")) return { code: "missing_email_column", columns };
    if (!record.email) return { code: "empty_email", row: index + 2 };
  }
  const email = firstDuplicate(records.map((record) => record.email ?? ""));
  if (email !== undefined) return { code: "duplicate_email", email };
  const userId = firstDuplicate(records.map((record) => record.userId).filter((id): id is string => !!id));
  if (userId !== undefined) return { code: "duplicate_user_id", userId };
  return null;
};

/** The translated message for a problem csvRecordsError found. */
export const csvRecordsErrorMessage = (error: CsvRecordsError, t: TFunction): string => {
  switch (error.code) {
    case "too_many_records":
      return t("workspace.contacts.upload_contacts_error_too_many_records", { max: error.max });
    case "missing_email_column":
      return t("workspace.contacts.upload_contacts_error_missing_email_column", {
        columns: error.columns.join(", "),
      });
    case "empty_email":
      return t("workspace.contacts.upload_contacts_error_empty_email", { row: error.row });
    case "duplicate_email":
      return t("workspace.contacts.upload_contacts_error_duplicate_email", { email: error.email });
    case "duplicate_user_id":
      return t("workspace.contacts.upload_contacts_error_duplicate_user_id", { userId: error.userId });
  }
};

/** The first key a column matches by name (or key), as forma2 suggested and the server falls back to. */
const matchingKey = (column: string, keys: AttributeKey[]) =>
  keys.find((key) => isStringMatch(column, attributeKeyLabel(key)));

/** The suggested mapping: each column to the id of the key it matches, or to itself, a key to create. */
export const suggestAttributeMap = (columns: string[], keys: AttributeKey[]): Record<string, string> =>
  Object.fromEntries(columns.map((column) => [column, matchingKey(column, keys)?.id ?? column]));

/** The columns the mapping step lists: those matching a key, or usable as a new key's name. */
export const mappableColumns = (columns: string[], keys: AttributeKey[]): string[] =>
  columns.filter((column) => matchingKey(column, keys) !== undefined || isSafeIdentifier(column));

/** The problems with a mapping that create keys: names that are not safe identifiers, and reserved names. */
export const attributeMapErrors = (
  columns: string[],
  attributeMap: Record<string, string>,
  keys: AttributeKey[],
  t: TFunction
): string[] => {
  const invalid: string[] = [];
  const reserved: string[] = [];
  for (const column of columns) {
    const target = attributeMap[column];
    if (!target || keys.some((key) => key.id === target)) continue;
    if (!isSafeIdentifier(target)) invalid.push(column);
    else if (isReservedAttributeKey(target)) reserved.push(column);
  }
  const errors: string[] = [];
  if (invalid.length > 0)
    errors.push(t("workspace.contacts.invalid_csv_column_names", { columns: invalid.join(", ") }));
  if (reserved.length > 0)
    errors.push(
      t("workspace.contacts.invalid_csv_reserved_column_names", {
        columns: reserved.join(", "),
        reservedKeys: RESERVED_ATTRIBUTE_KEYS_TEXT,
      })
    );
  return errors;
};

/** The columns mapped onto a target another column also fills; none may be. */
export const duplicateMappedColumns = (attributeMap: Record<string, string>): string[] => {
  const counts = new Map<string, number>();
  for (const target of Object.values(attributeMap)) counts.set(target, (counts.get(target) ?? 0) + 1);
  return Object.entries(attributeMap)
    .filter(([, target]) => (counts.get(target) ?? 0) > 1)
    .map(([column]) => column);
};

/** Whether a column's target creates a key: it names no existing key and could become one. */
export const isNewAttributeTarget = (target: string | undefined, keys: AttributeKey[]): boolean =>
  !!target && !keys.some((key) => key.id === target) && isSafeIdentifier(target);

export interface TypeMismatch {
  key: string;
  dataType: string;
  invalidValues: string[];
}

/** forma2's message for a column whose values do not fit its key's type: up to three samples and how many more. */
export const typeMismatchMessage = (mismatch: TypeMismatch, t: TFunction): string => {
  const sample = mismatch.invalidValues.slice(0, 3).join(", ");
  const more = mismatch.invalidValues.length - 3;
  return t("workspace.contacts.upload_contacts_error_attribute_type_mismatch", {
    key: mismatch.key,
    dataType: mismatch.dataType,
    values: `${sample}${more > 0 ? ` (${more} more)` : ""}`,
  });
};

/** forma2's example file. Its user_id, first_name and last_name columns have since become reserved, so it imports only once they are mapped onto the default keys. */
export const EXAMPLE_CSV = [
  "email,user_id,first_name,last_name,age,plan,signup_date",
  "user1@example.com,1001,John,Doe,28,premium,2024-01-15",
  "user2@example.com,1002,Jane,Smith,34,free,2024-02-20",
  "user3@example.com,1003,Mark,Jones,45,enterprise,2023-11-08",
  "user4@example.com,1004,Emily,Brown,22,premium,2024-03-01",
  "user5@example.com,1005,David,Wilson,31,free,2024-01-28",
].join("\n");
