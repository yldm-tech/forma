import type { NotificationOrganization, NotificationType } from "./queries/auth";

/** An email's unsubscribe link: /account/settings/notifications?type=alert|unsubscribedOrganizationIds&elementId=<id>. */
export interface UnsubscribeLink {
  type?: string;
  elementId?: string;
}

/**
 * The switch an unsubscribe link turns off, when it is still on: a survey's per-response alert, or an organization's auto-subscription. forma2 did this on loading the page so the link in the email works in one click; a link to something already off, or not on this page, does nothing.
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
  return null;
};
