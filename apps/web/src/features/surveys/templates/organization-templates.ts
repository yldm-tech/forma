import { queryOptions } from "@tanstack/react-query";
import type { TSurvey } from "@forma/types/surveys/types";
import { api } from "../../../lib/api";
import type { TemplatePreset } from "./preview";

// An organization's own survey templates: surveys its owners and managers saved as templates, offered in the gallery of every workspace of the organization (apps/server/internal/survey/orgtemplates.go).

/** One of the languages a template is translated into, by code: a workspace language belongs to one workspace, so using the template links or creates the language of that code. */
export interface OrganizationTemplateLanguage {
  code: string;
  alias: string | null;
  default: boolean;
  enabled: boolean;
}

/** The survey document a template creates, every translation included. */
export type OrganizationTemplatePreset = TemplatePreset &
  Partial<Pick<TSurvey, "styling" | "surveyClosedMessage">>;

/** A template as the gallery lists it; reading one template adds its preset. Timestamps are ISO strings. */
export interface OrganizationTemplate {
  id: string;
  createdAt: string;
  updatedAt: string;
  organizationId: string;
  name: string;
  description: string;
  /** Who saved it; null once that account is gone. */
  creator: { name: string } | null;
  questionCount: number;
  languages: OrganizationTemplateLanguage[];
  preset?: OrganizationTemplatePreset;
}

export interface OrganizationTemplateList {
  /** Newest first. */
  templates: OrganizationTemplate[];
  /** Whether the caller may rename and delete them: an owner or a manager, who are also the ones who save them. */
  canManage: boolean;
}

/** The name and description a template is saved or renamed with; the server trims them. */
export interface OrganizationTemplateDetails {
  name: string;
  description: string;
}

/** The bounds the server enforces on a template's name and description, in characters. */
export const TEMPLATE_NAME_MAX_LENGTH = 200;
export const TEMPLATE_DESCRIPTION_MAX_LENGTH = 1000;

export const organizationTemplateKeys = {
  list: (organizationId: string) => ["organizations", organizationId, "survey-templates"] as const,
  detail: (templateId: string) => ["survey-templates", templateId] as const,
};

export const organizationTemplatesQuery = (organizationId: string) =>
  queryOptions({
    queryKey: organizationTemplateKeys.list(organizationId),
    queryFn: ({ signal }) =>
      api.get<OrganizationTemplateList>(`/api/app/organizations/${organizationId}/survey-templates`, {
        signal,
      }),
  });

/** One template with its preset, for the gallery's preview. A template is never edited in place, only renamed, so its preset can be kept for the session. */
export const organizationTemplateQuery = (templateId: string) =>
  queryOptions({
    queryKey: organizationTemplateKeys.detail(templateId),
    queryFn: ({ signal }) =>
      api.get<OrganizationTemplate>(`/api/app/survey-templates/${templateId}`, { signal }),
    staleTime: Number.POSITIVE_INFINITY,
  });

/** Saves the survey as stored (not an editor's unsaved changes) as a template of its organization. */
export const saveSurveyAsTemplate = (surveyId: string, details: OrganizationTemplateDetails) =>
  api.post<OrganizationTemplate>(`/api/app/surveys/${surveyId}/save-as-template`, details);

export const updateOrganizationTemplate = (templateId: string, details: OrganizationTemplateDetails) =>
  api.patch<OrganizationTemplate>(`/api/app/survey-templates/${templateId}`, details);

export const deleteOrganizationTemplate = (templateId: string) =>
  api.delete<void>(`/api/app/survey-templates/${templateId}`);

export interface CreateFromOrganizationTemplateInput {
  templateId: string;
  workspaceId: string;
  surveyType: "link" | "app";
}

/** Creates a draft in the workspace from the template; answers the created survey. */
export const createSurveyFromOrganizationTemplate = ({
  templateId,
  workspaceId,
  surveyType,
}: CreateFromOrganizationTemplateInput) =>
  api.post<{ id: string }>(`/api/app/survey-templates/${templateId}/surveys`, { workspaceId, surveyType });

/** The gallery tells an organization template from a catalog one by this key, since both are chosen in one list. */
export const organizationTemplateKey = (templateId: string): string => `organization:${templateId}`;

/** The id of the organization template a gallery key names, or null for a catalog template. */
export const organizationTemplateIdOf = (key: string | null): string | null =>
  key?.startsWith("organization:") ? key.slice("organization:".length) : null;

/** The search the gallery applies to the catalog applies to these too: by name, case-insensitively. */
export const filterOrganizationTemplates = (
  templates: OrganizationTemplate[],
  search: string
): OrganizationTemplate[] =>
  search
    ? templates.filter((template) => template.name.toLowerCase().includes(search.toLowerCase()))
    : templates;
