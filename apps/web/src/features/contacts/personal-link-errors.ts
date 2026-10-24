import type { TFunction } from "i18next";
import { ApiError } from "../../lib/api";
import { errorMessage } from "../../lib/errors";

// The stable codes the server refuses a personal link with (apps/server/internal/contact/contactsurvey), forma2's personal-link-errors.ts.
export const SEGMENT_SURVEY_WORKSPACE_MISMATCH_ERROR_CODE = "segment_survey_workspace_mismatch";
export const CONTACT_SURVEY_WORKSPACE_MISMATCH_ERROR_CODE = "contact_survey_workspace_mismatch";
export const NO_CONTACTS_IN_SEGMENT_ERROR_CODE = "no_contacts_in_segment";

/** forma2's getTranslatedPersonalLinkError over a failed request: a personal-link code as its translated message, anything else as errorMessage shows it. */
export const personalLinkErrorMessage = (t: TFunction, error: unknown): string => {
  if (error instanceof ApiError) {
    switch (error.code) {
      case SEGMENT_SURVEY_WORKSPACE_MISMATCH_ERROR_CODE:
        return t("workspace.surveys.share.personal_links.segment_survey_workspace_mismatch");
      case CONTACT_SURVEY_WORKSPACE_MISMATCH_ERROR_CODE:
        return t("workspace.contacts.contact_survey_workspace_mismatch");
      case NO_CONTACTS_IN_SEGMENT_ERROR_CODE:
        return t("workspace.surveys.share.personal_links.no_contacts_in_segment");
    }
  }
  return errorMessage(t, error);
};
