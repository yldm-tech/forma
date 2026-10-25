import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { SparklesIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "../../../lib/cn";
import { getAIUnavailableMessage, offersEnableAI, workspaceAIQuery } from "../../ai/availability";
import { CreateWithAIDialog } from "./create-with-ai-dialog";

/**
 * forma2's CreateWithAITemplate: the template gallery's card that opens the create-with-AI dialog. Where AI is unavailable the card is disabled with the reason under it, as on the onboarding step, rather than opening a dialog that only says so; when the organization only has to switch AI on, a link to its settings sits under the reason.
 */
export const CreateWithAICard = ({
  workspaceId,
  organizationId,
  surveyType,
}: {
  workspaceId: string;
  organizationId?: string;
  surveyType: "link" | "app";
}) => {
  const { t, i18n } = useTranslation();
  const [open, setOpen] = useState(false);
  const ai = useQuery(workspaceAIQuery(workspaceId));
  const isAIAvailable = ai.data?.available ?? false;
  const unavailableReason = ai.isSuccess && !ai.data.available ? ai.data.reason : undefined;
  const reasonId = `create-with-ai-unavailable-${workspaceId}`;
  return (
    <>
      <div className="flex flex-col gap-2">
        <button
          type="button"
          disabled={!isAIAvailable}
          onClick={() => setOpen(true)}
          aria-describedby={ai.isSuccess && !isAIAvailable ? reasonId : undefined}
          className={cn(
            "group relative flex flex-1 flex-col rounded-lg border-2 border-dashed border-slate-300 bg-transparent p-6 text-left transition-colors duration-150",
            isAIAvailable ? "hover:border-slate-500" : "cursor-not-allowed opacity-50"
          )}>
          <SparklesIcon
            className={cn(
              "text-ink size-8 transition-all duration-150",
              isAIAvailable && "group-hover:scale-110"
            )}
          />
          <h3 className="text-md mt-3 mb-1 font-bold text-slate-700">
            {t("workspace.surveys.ai_create.card_title")}
          </h3>
          <p className="text-xs text-slate-600">{t("workspace.surveys.ai_create.card_description")}</p>
        </button>
        {ai.isSuccess && !isAIAvailable && (
          <p id={reasonId} className="text-xs text-slate-600">
            {getAIUnavailableMessage(unavailableReason, t)}
            {organizationId && offersEnableAI(unavailableReason) && (
              <>
                {" "}
                <Link
                  to="/organizations/$organizationId/settings/general"
                  params={{ organizationId }}
                  className="font-medium text-slate-800 underline underline-offset-2 hover:text-slate-950">
                  {t("common.ai_unavailable.enable_in_settings")}
                </Link>
              </>
            )}
          </p>
        )}
      </div>
      {open && (
        <CreateWithAIDialog
          open
          onOpenChange={setOpen}
          workspaceId={workspaceId}
          organizationId={organizationId}
          locale={i18n.language}
          surveyType={surveyType}
          isAIAvailable={isAIAvailable}
          aiUnavailableReason={ai.data?.reason}
        />
      )}
    </>
  );
};
