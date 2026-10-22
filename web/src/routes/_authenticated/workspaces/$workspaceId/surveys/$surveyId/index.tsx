import { createFileRoute, redirect } from "@tanstack/react-router";

// forma2's survey page opened the summary.
export const Route = createFileRoute("/_authenticated/workspaces/$workspaceId/surveys/$surveyId/")({
  beforeLoad: ({ params }) => {
    throw redirect({
      to: "/workspaces/$workspaceId/surveys/$surveyId/summary",
      params: { workspaceId: params.workspaceId, surveyId: params.surveyId },
      replace: true,
    });
  },
});
