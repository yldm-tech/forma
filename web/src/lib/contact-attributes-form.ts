import { z } from "zod";
import type { TFunction } from "i18next";
import type { AttributeKey } from "./attribute-keys";

// forma2's contact attribute editor rules (modules/contacts/types/contact.ts: ZEditContactAttributesForm and createEditContactAttributesSchema).

export interface AttributeRow {
  key: string;
  value: string;
}

export interface RowErrors {
  key?: string;
  value?: string;
}

const ZEmail = z.email();

/** The errors of each row, by index, and of the form as a whole. The first error set on a field wins, in forma2's order. */
export const attributeRowsErrors = (
  rows: AttributeRow[],
  keys: AttributeKey[],
  t: TFunction
): { rows: Record<number, RowErrors>; form?: string } => {
  const errors: Record<number, RowErrors> = {};
  const set = (index: number, field: keyof RowErrors, message: string) => {
    errors[index] ??= {};
    errors[index][field] ??= message;
  };
  if (rows.length === 0) return { rows: errors, form: "At least one attribute is required" };

  rows.forEach((row, index) => {
    if (row.key.length < 1) set(index, "key", "Key is required");
  });
  const indicesByKey = new Map<string, number[]>();
  rows.forEach((row, index) => {
    if (row.key) indicesByKey.set(row.key, [...(indicesByKey.get(row.key) ?? []), index]);
  });
  for (const [key, indices] of indicesByKey) {
    if (indices.length > 1) for (const index of indices) set(index, "key", `Duplicate key: ${key}`);
  }

  const emailIndex = rows.findIndex((row) => row.key === "email");
  const userIdIndex = rows.findIndex((row) => row.key === "userId");
  const filled = (index: number) => index !== -1 && rows[index]!.value.trim() !== "";
  if (!filled(emailIndex) && !filled(userIdIndex) && emailIndex !== -1 && userIdIndex !== -1) {
    set(emailIndex, "value", "Either email or userId is required");
    set(userIdIndex, "value", "Either email or userId is required");
  }
  rows.forEach((row, index) => {
    if (row.key === "email" && row.value.trim() !== "" && !ZEmail.safeParse(row.value).success) {
      set(index, "value", "Invalid email format");
    }
  });

  rows.forEach((row, index) => {
    const dataType = keys.find((key) => key.key === row.key)?.dataType;
    const hasValue = row.value.trim() !== "";
    if (dataType === "date") {
      if (!hasValue) set(index, "value", t("workspace.contacts.date_value_required"));
      else if (Number.isNaN(new Date(row.value).getTime()))
        set(index, "value", t("workspace.contacts.invalid_date_format"));
    } else if (dataType === "number") {
      if (!hasValue) set(index, "value", t("workspace.contacts.number_value_required"));
      else if (Number.isNaN(Number(row.value)))
        set(index, "value", t("workspace.contacts.invalid_number_format"));
    }
  });
  return { rows: errors };
};

/** The request body: number keys' values as numbers, every other value as typed. */
export const attributesPayload = (
  rows: AttributeRow[],
  keys: AttributeKey[]
): Record<string, string | number> =>
  Object.fromEntries(
    rows.map(({ key, value }) => {
      const dataType = keys.find((attributeKey) => attributeKey.key === key)?.dataType ?? "string";
      return [key, dataType === "number" && value !== "" ? Number(value) : value];
    })
  );

/** The `yyyy-MM-dd` a date input shows for a stored value, or "" when it names no valid day (forma2's parseStoredDay). */
export const storedDayForInput = (value: string): string => {
  const day = value.split("T")[0] ?? "";
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(day);
  if (!match) return "";
  const [, y, m, d] = match.map(Number) as [number, number, number, number];
  const date = new Date(Date.UTC(y, m - 1, d));
  return date.getUTCFullYear() === y && date.getUTCMonth() === m - 1 && date.getUTCDate() === d ? day : "";
};

/** forma2's toUTCDateString: a picked day stored as midnight UTC. */
export const dayToStoredValue = (day: string): string => {
  if (!day) return "";
  const [y, m, d] = day.split("-").map(Number) as [number, number, number];
  return new Date(Date.UTC(y, m - 1, d)).toISOString();
};

/** The translated text of a message the attribute update sends back. */
export const attributeUpdateMessage = (
  code: string,
  params: Record<string, string>,
  t: TFunction
): string => {
  switch (code) {
    case "email_or_userid_required":
      return t("workspace.contacts.attributes_msg_email_or_userid_required");
    case "attribute_type_validation_error":
      return t("workspace.contacts.attributes_msg_attribute_type_validation_error", params);
    case "email_already_exists":
      return t("workspace.contacts.attributes_msg_email_already_exists");
    case "userid_already_exists":
      return t("workspace.contacts.attributes_msg_userid_already_exists");
    case "attribute_limit_exceeded":
      return t("workspace.contacts.attributes_msg_attribute_limit_exceeded", params);
    case "new_attribute_created":
      return t("workspace.contacts.attributes_msg_new_attribute_created", params);
    default:
      return code;
  }
};
