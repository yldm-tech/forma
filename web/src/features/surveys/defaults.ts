import { normalizeLanguageCode } from "@forma/i18n-utils/canonical";
import {
  DEFAULT_SURVEY_LANGUAGE_CODE,
  isSurveyRuntimeLanguage,
} from "@forma/i18n-utils/survey-runtime-languages";
import { ZUserLocale } from "@forma/types/user";
import type { WorkspaceChannel } from "./list/filters";

/** The parts of a workspace's config the survey pages read (forma2's TWorkspaceConfig). The workspace endpoint returns the stored JSON as it is. */
export interface WorkspaceSurveyConfig {
  channel: WorkspaceChannel;
  defaultSurveyLanguage: string | null;
}

/** Reads the survey-relevant config of a workspace as GET /api/app/workspaces/{id} returns it (the stored JSON as it is); anything missing or malformed counts as unset. */
export const workspaceSurveyConfig = (workspace: object): WorkspaceSurveyConfig => {
  const raw = "config" in workspace ? workspace.config : undefined;
  const config = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  const channel = config.channel;
  return {
    channel: channel === "link" || channel === "app" || channel === "website" ? channel : null,
    defaultSurveyLanguage:
      typeof config.defaultSurveyLanguage === "string" ? config.defaultSurveyLanguage : null,
  };
};

/** The type of a survey created in a workspace: a website workspace makes app surveys, an unset channel link surveys (forma2's NewSurveyMenu and TemplateList). */
export const surveyTypeForChannel = (channel: WorkspaceChannel): "link" | "app" => {
  if (channel === "website" || channel === "app") return "app";
  return "link";
};

/**
 * forma2's resolveDefaultSurveyLanguage, the language a new survey is authored in: the workspace setting when the survey runtime has strings for it, else the creator's dashboard locale, else English. The setting is stored JSON, so a value the runtime cannot render is treated as unset rather than passed on.
 */
export const resolveDefaultSurveyLanguage = ({
  workspaceDefaultLanguage,
  userLocale,
}: {
  workspaceDefaultLanguage?: string | null;
  userLocale?: string | null;
}): string => {
  const workspaceDefault = workspaceDefaultLanguage
    ? normalizeLanguageCode(workspaceDefaultLanguage.trim())
    : null;
  if (workspaceDefault && isSurveyRuntimeLanguage(workspaceDefault)) return workspaceDefault;
  return ZUserLocale.safeParse(userLocale).data ?? DEFAULT_SURVEY_LANGUAGE_CODE;
};
