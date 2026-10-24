import { QueryClient } from "@tanstack/react-query";
import { expect, test } from "vite-plus/test";
import { surveyEditorKeys } from "../../features/survey-editor/api";
import { analysisKeys } from "../../features/survey-analysis/api";
import { forgetSurveySchedules } from "./organizations";
import { workspaceKeys } from "./workspaces";

test("forgetSurveySchedules drops cached surveys and marks workspaces stale after a scheduling zone change", async () => {
  const queryClient = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity } } });
  const surveyId = "survey-1";
  queryClient.setQueryData(surveyEditorKeys.survey(surveyId), { id: surveyId });
  queryClient.setQueryData(analysisKeys.survey(surveyId), { id: surveyId });
  queryClient.setQueryData(workspaceKeys.detail("workspace-1"), { id: "workspace-1" });

  await forgetSurveySchedules(queryClient);

  expect(queryClient.getQueryData(surveyEditorKeys.survey(surveyId))).toBeUndefined();
  expect(queryClient.getQueryData(analysisKeys.survey(surveyId))).toBeUndefined();
  expect(queryClient.getQueryState(workspaceKeys.detail("workspace-1"))?.isInvalidated).toBe(true);
});
