import type { ApiKey, ApiKeyPermission, OrganizationAccess } from "./queries/api-keys";

export interface PermissionRow {
  workspaceId: string;
  permission: ApiKeyPermission;
}

/** forma2's checkForDuplicatePermissions: a workspace may be granted once. */
export const hasDuplicateWorkspace = (rows: readonly PermissionRow[]): boolean =>
  new Set(rows.map((row) => row.workspaceId)).size !== rows.length;

/** forma2's isSubmitDisabled: a label that is not blank, and at least one workspace grant or organization access switch. */
export const canSubmitApiKey = (
  label: string,
  rows: readonly PermissionRow[],
  access: OrganizationAccess
): boolean =>
  label.trim() !== "" && (rows.length > 0 || access.accessControl.read || access.accessControl.write);

/** Write implies read: the read switch shows on, and is sent on, whenever write is. */
export const effectiveAccess = (access: OrganizationAccess): OrganizationAccess => ({
  accessControl: {
    read: access.accessControl.read || access.accessControl.write,
    write: access.accessControl.write,
  },
});

/** What a key may reach, as the API keys list summarises it in one line. */
export interface ApiKeyScope {
  /** The first workspace grants, in the server's order (by workspace name). */
  shown: ApiKey["workspacePermissions"];
  /** How many further workspace grants the line leaves out. */
  more: number;
  /** The organization access the key holds, write including read; null for none. */
  organization: "read" | "write" | null;
}

/** A key's scope for the list: at most limit workspace grants by name, the count of the rest, and its organization access read the way the server reads it (write implies read). */
export const apiKeyScope = (
  key: Pick<ApiKey, "workspacePermissions" | "organizationAccess">,
  limit = 2
): ApiKeyScope => {
  const access = effectiveAccess(key.organizationAccess).accessControl;
  return {
    shown: key.workspacePermissions.slice(0, limit),
    more: Math.max(0, key.workspacePermissions.length - limit),
    organization: access.write ? "write" : access.read ? "read" : null,
  };
};

/** The expiries the create form offers: none, a number of days from now, or the end of a chosen day. */
export type ApiKeyExpiryChoice = "never" | "7" | "30" | "90" | "365" | "custom";

export const API_KEY_EXPIRY_CHOICES: readonly ApiKeyExpiryChoice[] = [
  "never",
  "7",
  "30",
  "90",
  "365",
  "custom",
];

/** A day, as a date input gives it (YYYY-MM-DD), in the browser's time zone: the start of that day, or null for anything else. */
const localDay = (day: string): Date | null => {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(day);
  if (!match) return null;
  const date = new Date(Number(match[1]), Number(match[2]) - 1, Number(match[3]));
  // new Date rolls an impossible day (02-30) over into the next month; such a day is not a day.
  return date.getDate() === Number(match[3]) && date.getMonth() === Number(match[2]) - 1 ? date : null;
};

/** The earliest day the custom expiry may pick: tomorrow, in the browser's time zone, as a date input's min. */
export const firstCustomExpiryDay = (now: Date = new Date()): string => {
  const tomorrow = new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${tomorrow.getFullYear()}-${pad(tomorrow.getMonth() + 1)}-${pad(tomorrow.getDate())}`;
};

/** When a key created now with choice stops working: null for never, now plus the days, or the last second of the custom day in the browser's time zone. undefined when the custom day is missing, malformed or not after today, so the form cannot be sent. */
export const apiKeyExpiry = (
  choice: ApiKeyExpiryChoice,
  customDay: string,
  now: Date = new Date()
): Date | null | undefined => {
  if (choice === "never") return null;
  if (choice !== "custom") return new Date(now.getTime() + Number(choice) * 24 * 60 * 60 * 1000);
  const day = localDay(customDay);
  if (!day || customDay < firstCustomExpiryDay(now)) return undefined;
  return new Date(day.getFullYear(), day.getMonth(), day.getDate(), 23, 59, 59);
};

/** A key's expiry as the list shows it: none, still working, working for at most another week, or expired. */
export type ApiKeyExpiryState = "never" | "active" | "soon" | "expired";

const SOON_MS = 7 * 24 * 60 * 60 * 1000;

export const apiKeyExpiryState = (
  key: Pick<ApiKey, "expiresAt">,
  now: Date = new Date()
): ApiKeyExpiryState => {
  if (!key.expiresAt) return "never";
  const left = new Date(key.expiresAt).getTime() - now.getTime();
  if (left <= 0) return "expired";
  return left <= SOON_MS ? "soon" : "active";
};

/** A key by its hint, as the list tells keys apart: the prefix every key starts with, an ellipsis, and its last four characters. */
export const maskedApiKey = (hint: string): string => `fbk_…${hint}`;
