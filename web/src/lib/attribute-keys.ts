import type { TContactAttributeDataType } from "@forma/types/contact-attribute-key";

// forma2's contact attribute key rules shared by the attributes page, the CSV import and the contact editor (modules/contacts/lib/attribute-key-policy.ts, lib/utils/helper.ts).

/** A workspace's attribute key as the server returns it. */
export interface AttributeKey {
  id: string;
  createdAt: string;
  updatedAt: string;
  key: string;
  name: string | null;
  description: string | null;
  isUnique: boolean;
  type: "default" | "custom";
  dataType: TContactAttributeDataType;
  workspaceId: string;
}

/** Keys held back for the safe-identifier rename of the default keys; no custom key may take them. */
export const RESERVED_ATTRIBUTE_KEYS = ["user_id", "first_name", "last_name"] as const;

export const RESERVED_ATTRIBUTE_KEYS_TEXT = RESERVED_ATTRIBUTE_KEYS.join(", ");

export const isReservedAttributeKey = (key: string): boolean =>
  (RESERVED_ATTRIBUTE_KEYS as readonly string[]).includes(key.trim().toLowerCase());

/** The default keys a contact keeps whatever an edit removes. */
export const PROTECTED_ATTRIBUTE_KEYS = ["email", "userId", "firstName", "lastName"];

/** forma2's isStringMatch: value contains query, ignoring case, spaces, underscores and dashes. */
export const isStringMatch = (query: string, value: string): boolean => {
  const normalize = (s: string) =>
    s.toLowerCase().replaceAll(" ", "").replaceAll("_", "").replaceAll("-", "");
  return normalize(value).includes(normalize(query));
};

/** The label a key is shown with: its name, else the key itself. */
export const attributeKeyLabel = (key: Pick<AttributeKey, "key" | "name">): string => key.name ?? key.key;
