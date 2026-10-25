import { useSuspenseQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { PreviewSurvey } from "../../../components/ui/preview-survey/index";
import { workspaceQuery } from "../../../lib/queries/workspaces";
import { toEditorWorkspace } from "../../survey-editor/api";
import { surveyTemplatesInLanguageQuery } from "../api";
import { templatePreviewSurvey } from "./preview";

/**
 * forma2's template preview: the template rendered by the respondent runtime in preview mode, with the workspace's theme and branding, exactly as the editor's preview shows the draft it creates (a link survey full width, an app survey in a mock web app), with the editor's phone and desktop frames and its restart. The template is read from the catalog in the survey's default language, not the dashboard's, because that is the language "Use this template" creates the draft in. Answers go nowhere. Lazy-loaded by the gallery, so the preview's code stays out of the survey list until a template is previewed.
 */
export const TemplatePreview = ({
  workspaceId,
  templateId,
  defaultLanguage,
  surveyType,
}: {
  workspaceId: string;
  templateId: string;
  /** The default language of the survey the template would create (resolveDefaultSurveyLanguage). */
  defaultLanguage: string;
  surveyType: "link" | "app";
}) => {
  const { data: detail } = useSuspenseQuery(workspaceQuery(workspaceId));
  const { data: catalog } = useSuspenseQuery(surveyTemplatesInLanguageQuery(workspaceId, defaultLanguage));
  const workspace = useMemo(() => toEditorWorkspace(detail), [detail]);
  const template = catalog.templates.find((candidate) => candidate.id === templateId);
  const survey = useMemo(
    () => (template ? templatePreviewSurvey(template, surveyType) : null),
    [template, surveyType]
  );
  if (!survey) return null;
  return (
    <PreviewSurvey
      survey={survey}
      workspace={workspace}
      previewType={surveyType === "app" ? "modal" : "fullwidth"}
      languageCode="default"
      isSpamProtectionAllowed={false}
    />
  );
};
