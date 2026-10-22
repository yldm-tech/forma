import type { SurveyListItem } from "../api";
import type { SurveyStatus } from "./filters";

/** The statuses the list's "Change status" submenu offers: a draft is published from the editor, never from here. */
export const CHANGEABLE_STATUSES: SurveyStatus[] = ["inProgress", "paused", "completed"];

/** What a row of the survey list allows, forma2's SurveyCard and SurveyDropDownMenu rules. */
export interface SurveyRowRules {
  isArchived: boolean;
  /** A paused survey with a publish date is waiting to be published. */
  isScheduled: boolean;
  /** Where the row leads: the editor. forma2 opened a published or archived survey's summary, which is not ported yet. */
  href: string;
  /** A survey the member may not edit, or an archived one, has no page to open until the summary is ported. */
  isClickable: boolean;
  /** Only a read-only draft, which has no action at all, disables its menu; an archived survey keeps it for restore and delete. */
  isMenuDisabled: boolean;
  /** Edit, rename, duplicate, copy, delete, archive and restore. */
  canManage: boolean;
  canPreviewOrCopyLink: boolean;
  canChangeStatus: boolean;
  hasVisibleActions: boolean;
}

export const surveyRowRules = (
  survey: Pick<SurveyListItem, "id" | "status" | "type" | "archivedAt" | "publishOn">,
  workspaceId: string,
  isReadOnly: boolean
): SurveyRowRules => {
  const isArchived = survey.archivedAt !== null;
  const base = `/workspaces/${workspaceId}/surveys/${survey.id}`;
  const canManage = !isReadOnly;
  const canPreviewOrCopyLink = !isArchived && survey.type === "link" && survey.status !== "draft";
  const canChangeStatus = !isArchived && !isReadOnly && survey.status !== "draft";
  return {
    isArchived,
    isScheduled: !isArchived && survey.status === "paused" && survey.publishOn !== null,
    href: `${base}/edit`,
    isClickable: !isReadOnly && !isArchived,
    isMenuDisabled: survey.status === "draft" && isReadOnly,
    canManage,
    canPreviewOrCopyLink,
    canChangeStatus,
    hasVisibleActions: isArchived ? canManage : canManage || canPreviewOrCopyLink || canChangeStatus,
  };
};

/** The survey's public link, and its preview (forma2's `${publicDomain}/s/{id}`, with preview=true to open it without recording a response). */
export const surveyLink = (publicDomain: string, surveyId: string): string => `${publicDomain}/s/${surveyId}`;

export const surveyPreviewLink = (publicDomain: string, surveyId: string): string => {
  const url = new URL(surveyLink(publicDomain, surveyId));
  url.searchParams.set("preview", "true");
  return url.toString();
};
