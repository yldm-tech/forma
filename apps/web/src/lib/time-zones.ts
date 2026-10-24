/** The value the time zone menu uses for "no zone chosen", which the server stores as null and shows as UTC. */
export const UTC_DEFAULT = "UTC";

/** The value the survey scheduling time zone menu uses for "no zone chosen", which the server stores as null and resolves to the instance's zone. No IANA name looks like it. */
export const INSTANCE_DEFAULT = "instance-default";

/** The IANA zones the browser knows, for the organization's display time zone. UTC is offered once, as the default entry. */
export const ianaTimeZones = (): string[] =>
  Intl.supportedValuesOf("timeZone").filter((zone) => zone !== UTC_DEFAULT);

/** The IANA zones the browser knows, UTC included, for the organization's survey scheduling time zone, whose default entry is the instance's zone. The stored zone is offered too when the browser lists it under another name or not at all (V8 knows Asia/Calcutta and Europe/Kiev, not Asia/Kolkata and Europe/Kyiv), so the menu still shows the zone in effect. */
export const schedulingTimeZones = (stored?: string | null): string[] => {
  const supported = Intl.supportedValuesOf("timeZone");
  const zones = supported.includes(UTC_DEFAULT) ? supported : [UTC_DEFAULT, ...supported];
  return stored && !zones.includes(stored) ? [...zones, stored] : zones;
};

interface OrganizationSettings {
  name: string;
  displayTimeZone: string | null;
  surveySchedulingTimeZone: string | null;
}

/** The PATCH body for the fields of the organization form that changed and that this role may change: the name is the owner's, the time zones the owner's and managers'. Null when nothing is to be sent. */
export const organizationSettingsChanges = (
  initial: OrganizationSettings,
  current: OrganizationSettings,
  role: string | undefined
): Partial<OrganizationSettings> | null => {
  const isOwner = role === "owner";
  const canEditTimeZone = isOwner || role === "manager";
  const changes: Partial<OrganizationSettings> = {};
  if (isOwner && current.name.trim() !== initial.name) changes.name = current.name.trim();
  if (canEditTimeZone && current.displayTimeZone !== initial.displayTimeZone) {
    changes.displayTimeZone = current.displayTimeZone;
  }
  if (canEditTimeZone && current.surveySchedulingTimeZone !== initial.surveySchedulingTimeZone) {
    changes.surveySchedulingTimeZone = current.surveySchedulingTimeZone;
  }
  return Object.keys(changes).length > 0 ? changes : null;
};
