import { useNavigate } from "@tanstack/react-router";
import { ArrowLeftIcon, SearchIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../components/ui/button";
import { CreateWithAICard } from "../ai-create/create-with-ai-card";
import { TemplateList } from "./template-list";

/**
 * forma2's TemplateContainerWithPreview: the template gallery with its search, as the templates page (under a menu bar with a back button) or as the survey list of a workspace without any survey yet, which also offers a draft written by AI.
 */
export const TemplateGallery = ({
  workspaceId,
  organizationId,
  surveyType,
  defaultLanguage,
  isTemplatePage,
}: {
  workspaceId: string;
  organizationId?: string;
  surveyType: "link" | "app";
  defaultLanguage: string;
  isTemplatePage: boolean;
}) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [search, setSearch] = useState("");

  return (
    <div className="flex h-full flex-col">
      {isTemplatePage && (
        <div className="border-b border-slate-200 bg-white px-5 py-2.5">
          <Button variant="secondary" size="sm" onClick={() => void navigate({ href: "/" })}>
            <ArrowLeftIcon />
            {t("common.back")}
          </Button>
        </div>
      )}
      <div className="flex-1 overflow-auto bg-slate-50">
        <div className="mt-6 mb-3 ml-6 flex flex-col items-center justify-between md:flex-row md:items-end">
          <h1 className="text-2xl font-bold text-slate-800">
            {isTemplatePage
              ? t("workspace.surveys.templates.create_a_new_survey")
              : t("workspace.surveys.all_set_time_to_create_first_survey")}
          </h1>
          <label className="mx-6 flex h-8 items-center gap-2 rounded-lg border border-slate-700 bg-white px-4 text-slate-800">
            <SearchIcon className="size-4" aria-hidden="true" />
            <span className="sr-only">{t("common.search")}</span>
            <input
              type="search"
              className="w-full border-none bg-transparent text-sm focus:outline-hidden"
              placeholder={t("common.search")}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
          </label>
        </div>
        <TemplateList
          workspaceId={workspaceId}
          surveyType={surveyType}
          defaultLanguage={defaultLanguage}
          search={search}
          aiCard={
            isTemplatePage ? undefined : (
              <CreateWithAICard
                workspaceId={workspaceId}
                organizationId={organizationId}
                surveyType={surveyType}
              />
            )
          }
        />
      </div>
    </div>
  );
};
