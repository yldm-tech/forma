/** The value the time zone menu uses for "no zone chosen", which the server stores as null and shows as UTC. */
export const UTC_DEFAULT = "UTC";

/** The IANA zones the browser knows, for the organization's display time zone. UTC is offered once, as the default entry. */
export const ianaTimeZones = (): string[] => Intl.supportedValuesOf("timeZone").filter((zone) => zone !== UTC_DEFAULT);

/** The PATCH body for the fields of the organization form that changed and that this role may change: the name is the owner's, the time zone the owner's and managers'. Null when nothing is to be sent. */
export const organizationSettingsChanges = (
  initial: { name: string; displayTimeZone: string | null },
  current: { name: string; displayTimeZone: string | null },
  role: string | undefined
): { name?: string; displayTimeZone?: string | null } | null => {
  const isOwner = role === "owner";
  const canEditTimeZone = isOwner || role === "manager";
  const changes: { name?: string; displayTimeZone?: string | null } = {};
  if (isOwner && current.name.trim() !== initial.name) changes.name = current.name.trim();
  if (canEditTimeZone && current.displayTimeZone !== initial.displayTimeZone) {
    changes.displayTimeZone = current.displayTimeZone;
  }
  return Object.keys(changes).length > 0 ? changes : null;
};
