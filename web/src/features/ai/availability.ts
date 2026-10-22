import { queryOptions } from "@tanstack/react-query";
import { api } from "../../lib/api";

/** Why AI is unavailable on a surface, forma2's TAIUnavailableReason (server/internal/ai/service.go). */
export type AIUnavailableReason = "not_in_plan" | "not_enabled" | "instance_not_configured" | "read_only";

/** Whether a surface may offer AI, and if not why. */
export interface AIAvailability {
  available: boolean;
  reason?: AIUnavailableReason;
}

/** An organization's AI configuration with its availability, for the organization settings' AI toggle. */
export interface OrganizationAI extends AIAvailability {
  organizationId: string;
  isAISmartToolsEnabled: boolean;
  isAISmartToolsEntitled: boolean;
  isInstanceConfigured: boolean;
}

export const aiKeys = {
  organization: (organizationId: string) => ["organizations", organizationId, "ai"] as const,
  workspace: (workspaceId: string) => ["workspaces", workspaceId, "ai"] as const,
  surveyTranslation: (surveyId: string) => ["surveys", surveyId, "ai-translation"] as const,
};

/** GET /api/app/organizations/{id}/ai. */
export const organizationAIQuery = (organizationId: string) =>
  queryOptions({
    queryKey: aiKeys.organization(organizationId),
    queryFn: ({ signal }) =>
      api.get<OrganizationAI>(`/api/app/organizations/${organizationId}/ai`, { signal }),
  });

/** GET /api/app/workspaces/{id}/ai, forma2's getSurveyAIAvailability: read_only for a member who cannot write to the workspace. */
export const workspaceAIQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: aiKeys.workspace(workspaceId),
    queryFn: ({ signal }) => api.get<AIAvailability>(`/api/app/workspaces/${workspaceId}/ai`, { signal }),
  });

/** GET /api/app/surveys/{id}/ai/translation-availability, forma2's checkAITranslationAvailableAction. */
export const surveyTranslationAIQuery = (surveyId: string) =>
  queryOptions({
    queryKey: aiKeys.surveyTranslation(surveyId),
    queryFn: ({ signal }) =>
      api.get<AIAvailability>(`/api/app/surveys/${surveyId}/ai/translation-availability`, { signal }),
  });

export interface AITranslationField {
  path: string;
  defaultText: string;
  isRichText: boolean;
}

/** What a translation run produced: the translations by path, and the paths that did not come back. */
export interface AITranslationResult {
  translations: Record<string, string>;
  failedPaths: string[];
}

/** POST /api/app/workspaces/{id}/ai/translations, forma2's translateSurveyFieldsAction. The languages are the names the editor shows. */
export const translateSurveyFields = (
  workspaceId: string,
  input: { fields: AITranslationField[]; sourceLanguage: string; targetLanguage: string }
) => api.post<AITranslationResult>(`/api/app/workspaces/${workspaceId}/ai/translations`, input);

/** Only plain key lookups are needed, so the narrowest translate signature does. */
type Translate = (key: string) => string;

/** forma2's getAIUnavailableAction: switching AI on in the organization settings is the only reason a user can resolve themselves. This server has no plans, so not_in_plan has no billing page to send anyone to. */
export const offersEnableAI = (reason: AIUnavailableReason | undefined): boolean => reason === "not_enabled";

/** forma2's getAIUnavailableMessage: the one wording for "AI is unavailable" every AI surface shows. */
export const getAIUnavailableMessage = (reason: AIUnavailableReason | undefined, t: Translate): string => {
  switch (reason) {
    case "not_in_plan":
      return t("common.ai_unavailable.not_in_plan");
    case "not_enabled":
      return t("common.ai_unavailable.not_enabled");
    case "instance_not_configured":
      return t("common.ai_unavailable.instance_not_configured");
    case "read_only":
      return t("common.ai_unavailable.read_only");
    default:
      return t("common.ai_unavailable.unknown");
  }
};

/** The server codes that also mean "AI is unavailable": the organization's toggle or the instance's configuration changed between page load and the request. */
const ERROR_CODE_REASONS = new Map<string, AIUnavailableReason>([
  ["ai_features_not_enabled", "not_in_plan"],
  ["ai_smart_tools_disabled", "not_enabled"],
  ["ai_instance_not_configured", "instance_not_configured"],
]);

/** forma2's getAIUnavailableMessageForErrorCode: undefined for any other code, so callers keep their own handling. */
export const getAIUnavailableMessageForErrorCode = (
  errorCode: string | undefined,
  t: Translate
): string | undefined => {
  const reason = errorCode ? ERROR_CODE_REASONS.get(errorCode) : undefined;
  return reason ? getAIUnavailableMessage(reason, t) : undefined;
};
