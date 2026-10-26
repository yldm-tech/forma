import type { NotificationOrganization, NotificationSurvey, NotificationType } from "./queries/auth";

/** An email's unsubscribe link: /account/settings/notifications?type=alert|unsubscribedOrganizationIds|unsubscribedWeeklySummaryOrganizationIds&elementId=<id>. */
export interface UnsubscribeLink {
  type?: string;
  elementId?: string;
}

/**
 * The switch an unsubscribe link turns off, when it is still on: a survey's per-response alert, an organization's auto-subscription, or an organization's weekly summary. forma2 did this on loading the page so the link in the email works in one click; a link to something already off, or not on this page, does nothing.
 */
export const unsubscribeTarget = (
  link: UnsubscribeLink,
  organizations: readonly NotificationOrganization[]
): { type: NotificationType; id: string } | null => {
  if (!link.elementId) return null;
  if (link.type === "alert") {
    const survey = organizations.flatMap((org) => org.surveys).find((s) => s.id === link.elementId);
    return survey?.alert ? { type: "alert", id: survey.id } : null;
  }
  if (link.type === "unsubscribedOrganizationIds") {
    const organization = organizations.find((org) => org.id === link.elementId);
    return organization?.autoSubscribe ? { type: "unsubscribedOrganizationIds", id: organization.id } : null;
  }
  if (link.type === "unsubscribedWeeklySummaryOrganizationIds") {
    const organization = organizations.find((org) => org.id === link.elementId);
    return organization?.weeklySummary
      ? { type: "unsubscribedWeeklySummaryOrganizationIds", id: organization.id }
      : null;
  }
  return null;
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
