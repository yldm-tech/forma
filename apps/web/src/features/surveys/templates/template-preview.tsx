import { useSuspenseQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { PreviewSurvey } from "../../../components/ui/preview-survey/index";
import { workspaceQuery } from "../../../lib/queries/workspaces";
import { toEditorWorkspace } from "../../survey-editor/api";
import type { SurveyTemplate } from "../api";
import { templatePreviewSurvey } from "./preview";

/**
 * forma2's template preview: the template rendered by the respondent runtime in preview mode, with the workspace's theme and branding, exactly as the editor's preview shows the draft it creates (a link survey full width, an app survey in a mock web app), with the editor's phone and desktop frames and its restart. Answers go nowhere. Lazy-loaded by the gallery, so the preview's code stays out of the survey list until a template is previewed.
 */
export const TemplatePreview = ({
  workspaceId,
  template,
  surveyType,
}: {
  workspaceId: string;
  template: SurveyTemplate;
  surveyType: "link" | "app";
}) => {
  const { data: detail } = useSuspenseQuery(workspaceQuery(workspaceId));
  const workspace = useMemo(() => toEditorWorkspace(detail), [detail]);
  const survey = useMemo(() => templatePreviewSurvey(template, surveyType), [template, surveyType]);
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
