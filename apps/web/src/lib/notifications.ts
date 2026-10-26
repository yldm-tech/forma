import type { NotificationOrganization, NotificationSurvey, NotificationType } from "./queries/auth";

/** An email's unsubscribe link: /account/settings/notifications?type=alert|unsubscribedOrganizationIds|unsubscribedWeeklySummaryOrganizationIds&elementId=<id>. */
export interface UnsubscribeLink {
  type?: string;
  elementId?: string;
}

/** Whether a link names a switch at all: both a type and an id, as every unsubscribe link in an email carries. */
export const isUnsubscribeLink = (link: UnsubscribeLink): link is Required<UnsubscribeLink> =>
  !!link.type && !!link.elementId;

/** The switch an unsubscribe link names, as the notification settings have it now: its kind, the survey or organization it is for, that one's name, and whether it is on. */
export interface UnsubscribeSubject {
  type: NotificationType;
  id: string;
  name: string;
  enabled: boolean;
}

/** The switch an unsubscribe link names, or null when it names none the user has: an unknown type, or a survey or organization not on the notifications page (gone, or out of the user's reach). */
export const unsubscribeSubject = (
  link: UnsubscribeLink,
  organizations: readonly NotificationOrganization[]
): UnsubscribeSubject | null => {
  if (!link.elementId) return null;
  if (link.type === "alert") {
    const survey = organizations.flatMap((org) => org.surveys).find((s) => s.id === link.elementId);
    return survey ? { type: "alert", id: survey.id, name: survey.name, enabled: survey.alert } : null;
  }
  if (
    link.type === "unsubscribedOrganizationIds" ||
    link.type === "unsubscribedWeeklySummaryOrganizationIds"
  ) {
    const organization = organizations.find((org) => org.id === link.elementId);
    if (!organization) return null;
    const enabled =
      link.type === "unsubscribedOrganizationIds" ? organization.autoSubscribe : organization.weeklySummary;
    return { type: link.type, id: organization.id, name: organization.name, enabled };
  }
  return null;
};

/**
 * The switch an unsubscribe link turns off, when it is still on: a survey's per-response alert, an organization's auto-subscription, or an organization's weekly summary. forma2 did this on loading the page so the link in the email works in one click; a link to something already off, or not on this page, does nothing.
 */
export const unsubscribeTarget = (
  link: UnsubscribeLink,
  organizations: readonly NotificationOrganization[]
): { type: NotificationType; id: string } | null => {
  const subject = unsubscribeSubject(link, organizations);
  return subject?.enabled ? { type: subject.type, id: subject.id } : null;
};

/** Pinned to one locale so the order does not depend on the browser's; numeric keeps "Workspace 2" ahead of "Workspace 10". */
const collator = new Intl.Collator("en", { numeric: true });

/** forma2's getAlertRows: an organization's surveys by workspace name, then survey name, then id, so a workspace's surveys stay together and two same-named surveys never swap places. */
export const alertRows = (surveys: readonly NotificationSurvey[]): NotificationSurvey[] =>
  [...surveys].sort(
    (a, b) =>
      collator.compare(a.workspaceName, b.workspaceName) ||
      collator.compare(a.name, b.name) ||
      collator.compare(a.id, b.id)
  );
