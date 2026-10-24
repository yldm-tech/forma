import type { TContactAttributeDataType } from "@forma/types/contact-attribute-key";
import { type AttributeKey, PROTECTED_ATTRIBUTE_KEYS } from "./attribute-keys";
import { formatDateForDisplay } from "./time";

// forma2's contact display rules (modules/contacts/lib/format-attribute-value.ts, utils.ts, components/contact-data-view.tsx and contact-table-column.tsx).

/** forma2's formatAttributeValue: "-" for nothing, a date in the locale's short form, a number with its locale's grouping, anything else as it is. */
export const formatAttributeValue = (
  value: string | number | null | undefined,
  dataType: TContactAttributeDataType,
  locale: string
): string => {
  if (value === null || value === undefined || value === "") return "-";
  if (dataType === "date") {
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? String(value) : formatDateForDisplay(date, locale);
  }
  if (dataType === "number") {
    const num = typeof value === "number" ? value : Number.parseFloat(value);
    return Number.isNaN(num) ? String(value) : num.toLocaleString(locale);
  }
  return String(value);
};

/** forma2's getContactIdentifier: the email, else the userId, else nothing. */
export const contactIdentifier = (attributes: Record<string, string | undefined>): string =>
  attributes.email || attributes.userId || "";

export interface ContactTableRow {
  id: string;
  userId: string;
  email: string;
  firstName: string;
  lastName: string;
  attributes: { key: string; name: string | null; value: string; dataType: TContactAttributeDataType }[];
}

/** The table's rows: the four default keys as their own columns, then every other key of the workspace. */
export const contactTableRows = (
  contacts: { id: string; attributes: Record<string, string> }[],
  keys: AttributeKey[]
): ContactTableRow[] => {
  const otherKeys = keys.filter((key) => !PROTECTED_ATTRIBUTE_KEYS.includes(key.key));
  return contacts.map((contact) => ({
    id: contact.id,
    userId: contact.attributes.userId ?? "",
    email: contact.attributes.email ?? "",
    firstName: contact.attributes.firstName ?? "",
    lastName: contact.attributes.lastName ?? "",
    attributes: otherKeys.map((key) => ({
      key: key.key,
      name: key.name,
      value: contact.attributes[key.key] ?? "",
      dataType: key.dataType,
    })),
  }));
};

/** forma2's first-visit column visibility: selection, email and names shown, the user id only when some contact has one, every other column hidden. */
export const defaultColumnVisibility = (
  columnIds: string[],
  rows: ContactTableRow[]
): Record<string, boolean> => ({
  ...Object.fromEntries(columnIds.map((id) => [id, false])),
  userId: rows.some((row) => row.userId),
  select: true,
  email: true,
  firstName: true,
  lastName: true,
});

/** The pieces of a value around each case-insensitive occurrence of the search, flagged when they match. */
export const highlightParts = (value: string, search: string): { text: string; match: boolean }[] => {
  const query = search.trim();
  if (!query) return [{ text: value, match: false }];
  const regex = new RegExp(`(${search.replaceAll(/[-/\\^$*+?.()|[\]{}]/g, "\\$&")})`, "gi");
  return value
    .split(regex)
    .filter((text) => text !== "")
    .map((text) => ({ text, match: text.toLowerCase() === search.toLowerCase() }));
};

/** The cells of a row that the table hides but whose value contains the search (as the server matches it: trimmed, case-insensitive), so a hit the visible columns do not explain can be. */
export const hiddenColumnMatches = <C extends { id: string; value: string }>(
  cells: C[],
  search: string,
  columnVisibility: Record<string, boolean>
): C[] => {
  const query = search.trim().toLowerCase();
  if (!query) return [];
  return cells.filter(
    (cell) => columnVisibility[cell.id] === false && cell.value.toLowerCase().includes(query)
  );
};
