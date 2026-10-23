import { useQuery } from "@tanstack/react-query";
import { SparklesIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { workspaceAIQuery } from "../../ai/availability";
import { CreateWithAIDialog } from "./create-with-ai-dialog";

/** forma2's CreateWithAITemplate: the template gallery's card that opens the create-with-AI dialog. */
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
  const ai = useQuery(workspaceAIQuery(workspaceId)).data;
  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="group hover:border-brand-dark relative flex flex-col rounded-lg border-2 border-dashed border-slate-300 bg-transparent p-6 text-left transition-colors duration-150">
        <SparklesIcon className="text-brand-dark size-8 transition-all duration-150 group-hover:scale-110" />
        <h3 className="text-md mt-3 mb-1 font-bold text-slate-700">
          {t("workspace.surveys.ai_create.card_title")}
        </h3>
        <p className="text-xs text-slate-600">{t("workspace.surveys.ai_create.card_description")}</p>
      </button>
      {open && (
        <CreateWithAIDialog
          open
          onOpenChange={setOpen}
          workspaceId={workspaceId}
          organizationId={organizationId}
          locale={i18n.language}
          surveyType={surveyType}
          isAIAvailable={ai?.available ?? false}
          aiUnavailableReason={ai?.reason}
        />
      )}
    </>
  );
};
