import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { RoutePending } from "../../../../../../components/shell/route-states";
import { workspaceAIQuery } from "../../../../../../features/ai/availability";
import { leaveIfOnboardingComplete, onboardingPath } from "../../../../../../features/onboarding/onboarding";
import { CreateWithAIForm } from "../../../../../../features/surveys/ai-create/create-with-ai-form";
import { surveyEditorPath } from "../../../../../../features/surveys/templates/use-create-survey";
import { guardPageError } from "../../../../../../lib/route-guards";

// forma2's AI onboarding step: the create-with-AI form as a page of its own, left for the first-survey step whenever AI is unavailable and for the workspace once it has a survey.
export const Route = createFileRoute("/_authenticated/organizations/$organizationId/workspaces/new/ai")({
  beforeLoad: ({ context: { onboarding } }) => leaveIfOnboardingComplete(onboarding),
  loader: async ({ context: { queryClient, onboarding }, params }) => {
    const ai = await queryClient.fetchQuery(workspaceAIQuery(onboarding.workspaceId)).catch(guardPageError);
    if (!ai.available)
      throw redirect({ href: onboardingPath(params.organizationId, "survey"), replace: true });
    return onboarding;
  },
  pendingComponent: RoutePending,
  component: AIStep,
});

function AIStep() {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const { organizationId } = Route.useParams();
  const onboarding = Route.useLoaderData();
  return (
    <main className="flex min-h-screen min-w-full flex-col items-center justify-center bg-slate-50 p-6">
      <div className="flex w-full max-w-xl flex-col items-center gap-y-8 px-4">
        <div className="text-center">
          <h1 className="text-3xl font-medium text-slate-800">
            {t("workspace.surveys.ai_create.dialog_title")}
          </h1>
          <p className="mt-2 text-sm text-slate-500">{t("workspace.surveys.ai_create.dialog_description")}</p>
        </div>
        <div className="h-[28rem] w-full">
          <CreateWithAIForm
            workspaceId={onboarding.workspaceId}
            organizationId={organizationId}
            locale={i18n.language}
            isAIAvailable
            promptInputRef={(node) => node?.focus()}
            onSuccess={(surveyId) =>
              void navigate({ href: surveyEditorPath(onboarding.workspaceId, surveyId, "cx") })
            }
          />
        </div>
      </div>
    </main>
  );
}
